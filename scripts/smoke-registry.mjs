// The round trip, after the tag: the same consumer the packed smoke
// installs, installed this time from npm and from the module proxy, with
// nothing laid for it and nothing overridden.
//
//	node scripts/smoke-registry.mjs v0.3.0
//	node scripts/smoke-registry.mjs v0.3.0 --open-issue   # and file one if it fails
//
// It is skipped, loudly and successfully, until the tag it is given exists
// in this checkout: before that there is nothing on either registry to
// install and the question it asks has no answer yet. The release workflow
// runs it on a tag push and never on a rehearsal, which is why the
// rehearsal keeps scripts/smoke-packed.mjs beside it — that one answers
// the shape of what will be published, this one answers what was.
//
// Its failure fails the run, and with --open-issue it also files an issue
// naming the tag. By then the tag exists and the packages are on npm, so
// nothing can be taken back and nobody re-runs a red job on a tag: an
// issue is what is left of the failure the next morning.
//
// The steps are functions and the script runs only when it is the program,
// so that a test can exercise a step — the nested-module consumers, the
// outsider's environment — without a registry: a step nothing exercises
// before the tag is found out after it (v0.6.0, #611).
import { mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { execFileSync, execSync, spawn } from "node:child_process";
import { connect, createServer } from "node:net";
import { setTimeout as after } from "node:timers/promises";
import { dirname, join } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { copyRegistryConsumer, examples, modules, root } from "./packages.mjs";
import { waitForRegistries } from "./registry.mjs";
import { holdProbeExchange } from "./probe-exchange.mjs";
import { addPublishedDependencies, holdOutsiderImports } from "./smoke-imports.mjs";

export const module = "github.com/Bitspark/nightseam";
const step = message => console.log("round trip:", message);

/**
 * The environment the outsider installs in: no credential and no user
 * configuration, because the question is whether an installation with
 * nothing laid for it resolves. setup-node points NPM_CONFIG_USERCONFIG at
 * an .npmrc whose token line reads NODE_AUTH_TOKEN whether or not the step
 * set it, a first publish sets a real one, and a maintainer's .npmrc may
 * route a scope elsewhere; pnpm sends whatever it is handed, and each
 * answers a different question from the consumer's.
 */
export function outsiderEnvironment(scratch, base = process.env) {
  const env = { ...base };
  for (const name of Object.keys(env)) {
    if (/^(NODE_AUTH_TOKEN|NPM_TOKEN|NPM_CONFIG_USERCONFIG|npm_config_userconfig)$/i.test(name)) delete env[name];
  }
  const userconfig = join(scratch, "outsider.npmrc");
  writeFileSync(userconfig, "");
  env.npm_config_userconfig = userconfig;
  return env;
}

/** The commands, run in one environment; an option's `env` is laid over it. */
export function commands(env) {
  const run = (program, args, options = {}) =>
    execFileSync(program, args, { encoding: "utf8", stdio: ["ignore", "pipe", "inherit"], ...options, env: { ...env, ...options.env } });
  // On Windows pnpm is a .cmd, which Node refuses to spawn as a file and
  // runs as a command line instead.
  const quote = argument => (/[\s"]/.test(argument) ? `"${argument}"` : argument);
  const pnpm = (args, options = {}) => {
    const common = { encoding: "utf8", stdio: ["ignore", "pipe", "inherit"], ...options, env: { ...env, ...options.env } };
    return process.platform === "win32" ? execSync(["pnpm.cmd", ...args.map(quote)].join(" "), common) : execFileSync("pnpm", args, common);
  };
  return { run, pnpm };
}

const probe = "examples/probe";

export async function main(argv = process.argv.slice(2)) {
  const tag = argv[0];
  if (!/^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/.test(tag ?? "")) {
    console.error("usage: node scripts/smoke-registry.mjs v<major.minor.patch> [--open-issue]");
    return 2;
  }
  if (!tagged(tag)) {
    console.log(`round trip: skipped — ${tag} is not a tag in this checkout, so there is nothing published to install; it runs after the tag is pushed`);
    return 0;
  }
  if (!examples.includes(probe)) {
    console.error("missing examples/probe; the registry round trip requires the generated WebSocket consumer");
    return 1;
  }
  const scratch = mkdtempSync(join(tmpdir(), "nightseam-roundtrip-"));
  const held = {};
  try {
    await roundTrip({ tag, scratch, held, ...commands(outsiderEnvironment(scratch)) });
    return 0;
  } catch (failure) {
    report(failure, tag, argv);
    throw failure;
  } finally {
    if (held.server) stop(held.server);
    rmSync(scratch, { recursive: true, force: true, maxRetries: 10 });
  }
}

export async function roundTrip({ tag, scratch, held, run, pnpm }) {
  const version = tag.slice(1);
  // A successful publish precedes registry propagation. Wait for the exact
  // release everywhere, then install once; failures still reach report().
  await waitForRegistries(tag, { log: step });
  const consumer = join(scratch, "consumer");
  step(`copying ${probe} to a consumer outside the workspace`);
  copyRegistryConsumer(join(root, probe), consumer);
  // Include packages the example does not use, pinned to this release, so
  // registry availability is followed by an actual import of every entry.
  addPublishedDependencies(consumer, version);

  // Nothing is overridden and no proxy is laid: the manifest asks for the
  // version and the registry answers, or this is where the release is
  // found out.
  step(`pnpm install — @nightseam/* at ${version}, from npm, as an outsider with no credential`);
  pnpm(["install", "--no-frozen-lockfile"], { cwd: consumer, stdio: ["ignore", "inherit", "inherit"] });
  step("pnpm check");
  pnpm(["check"], { cwd: consumer, stdio: ["ignore", "inherit", "inherit"] });
  holdOutsiderImports(consumer, { pnpm, run, step });

  // The example carries no go.sum — the hash of a version nobody had
  // tagged was not knowable when it was written — so the consumer writes
  // one from what the proxy serves for the tag.
  const go = { GOWORK: "off", GOFLAGS: "-mod=mod" };
  step(`go get ${module}@${tag}`);
  run("go", ["get", `${module}@${tag}`], { cwd: consumer, env: go, stdio: ["ignore", "inherit", "inherit"] });
  run("go", ["mod", "download", "all"], { cwd: consumer, env: go, stdio: ["ignore", "inherit", "inherit"] });
  step("go tool nightseam check");
  run("go", ["tool", "nightseam", "check"], { cwd: consumer, env: go, stdio: ["ignore", "inherit", "inherit"] });
  nested({ tag, scratch, go, run });

  const address = `127.0.0.1:${await free()}`;
  step(`go run ./server on ws://${address}/probe`);
  held.server = spawn("go", ["run", "./server"], {
    cwd: consumer,
    env: { ...process.env, ...go, PROBE_ADDRESS: address },
    stdio: ["ignore", "inherit", "inherit"],
    detached: process.platform !== "win32",
  });
  const deadline = Date.now() + 180_000;
  while (!(await listening(address))) {
    if (Date.now() > deadline || held.server.exitCode !== null) throw new Error(`the example's server never listened on ${address}`);
    await after(200);
  }

  step("pnpm start");
  const out = pnpm(["start"], { cwd: consumer, env: { PROBE_URL: `ws://${address}/probe` } });
  process.stdout.write(out);
  holdProbeExchange(out);
  console.log(`round trip: ${tag} installed from npm and from the proxy, and ${probe} ran against it`);
}

/**
 * Every nested module — the OpenTelemetry adapter, the authority profile —
 * is a module of its own and released by a tag of its own beside this one:
 * it is the one release step that depends on the first tag already being
 * fetchable, so it is the one most worth asking about here. A module of its
 * own needs a consumer of its own, since the example asks for none of it;
 * each consumer imports the module's root package and, where one is
 * exported, calls something of it.
 */
export function consumer(directory) {
  return {
    "otel/go": 'import otel "github.com/Bitspark/nightseam/otel/go"\n\nfunc main() { _ = otel.Propagator(nil) }\n',
    "auth/go": 'import _ "github.com/Bitspark/nightseam/auth/go"\n\nfunc main() {}\n',
  }[directory];
}

export function nested({ tag, scratch, go, run, log = step, nestedModules = modules }) {
  for (const file of nestedModules) {
    const directory = dirname(file);
    const program = consumer(directory);
    if (!program) throw new Error(`no registry consumer for the nested module ${directory}; add one beside the others`);
    const at = join(scratch, directory.replace("/", "-"));
    mkdirSync(at, { recursive: true });
    writeFileSync(join(at, "go.mod"), `module example.com/${directory.replace("/", "-")}\n\ngo ${directive()}\n`);
    writeFileSync(join(at, "main.go"), `package main\n\n${program}`);
    log(`go get ${module}/${directory}@${tag}`);
    run("go", ["get", `${module}/${directory}@${tag}`], { cwd: at, env: go, stdio: ["ignore", "inherit", "inherit"] });
    run("go", ["build", "./..."], { cwd: at, env: go, stdio: ["ignore", "inherit", "inherit"] });
  }
}

/** The language version this repository's own module declares, which no consumer of it may declare less than. */
function directive() {
  return readFileSync(join(root, "go.mod"), "utf8").match(/^go (\S+)$/m)[1];
}

function tagged(name) {
  try {
    return execFileSync("git", ["tag", "--list", name], { cwd: root, encoding: "utf8" }).trim() === name;
  } catch {
    return false;
  }
}

/**
 * The issue the failure leaves behind. `gh` and a token are the workflow's;
 * without them this says what it would have filed and lets the failure
 * stand on its own, which is what a run by hand wants.
 */
function report(failure, tag, argv) {
  if (!argv.includes("--open-issue")) return;
  const title = `release: ${tag} is published and the round trip against it failed`;
  const body = [
    `\`node scripts/smoke-registry.mjs ${tag}\` failed after ${tag} was published.`,
    "",
    "The tag exists and the packages are on npm, so this is not a release that can be re-cut: either what is published is usable and the smoke is wrong, or it is not and a patch release is the answer.",
    "",
    "```",
    String(failure.message ?? failure).slice(0, 4000),
    "```",
    "",
    "RELEASING.md, *Cutting a release*, says what this step is for.",
  ].join("\n");
  try {
    execFileSync("gh", ["issue", "create", "--title", title, "--body", body], { stdio: ["ignore", "inherit", "inherit"] });
  } catch {
    console.error(`round trip: could not file an issue; it would have been titled ${JSON.stringify(title)}`);
  }
}

/** A port nothing is listening on, which the example's server is then told to take. */
function free() {
  return new Promise((resolve, reject) => {
    const socket = createServer();
    socket.on("error", reject);
    socket.listen(0, "127.0.0.1", () => {
      const { port } = socket.address();
      socket.close(() => resolve(port));
    });
  });
}

/** Whether anything is accepting connections there yet, which is what the server having started looks like from here. */
function listening(address) {
  const [host, port] = address.split(":");
  return new Promise(resolve => {
    const probe = connect(Number(port), host);
    probe.on("connect", () => {
      probe.destroy();
      resolve(true);
    });
    probe.on("error", () => resolve(false));
  });
}

/** Stops the server and everything it started: `go run` is a parent of the program it built. */
function stop(child) {
  try {
    if (process.platform === "win32") execFileSync("taskkill", ["/T", "/F", "/PID", String(child.pid)], { stdio: "ignore" });
    else process.kill(-child.pid, "SIGTERM");
  } catch {
    // It had already gone, which is the other way this ends.
  }
}

if (process.argv[1] && realpathSync(process.argv[1]) === realpathSync(fileURLToPath(import.meta.url))) {
  process.exitCode = await main();
}
