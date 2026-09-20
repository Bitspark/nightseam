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
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { execFileSync, execSync, spawn } from "node:child_process";
import { connect, createServer } from "node:net";
import { setTimeout as after } from "node:timers/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { copyRegistryConsumer, examples, root } from "./packages.mjs";
import { waitForRegistries } from "./registry.mjs";
import { holdProbeExchange } from "./probe-exchange.mjs";

const tag = process.argv[2];
if (!/^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/.test(tag ?? "")) {
  console.error("usage: node scripts/smoke-registry.mjs v<major.minor.patch> [--open-issue]");
  process.exit(2);
}
const version = tag.slice(1);
const module = "github.com/Bitspark/nightseam";
const step = message => console.log("round trip:", message);
const run = (program, args, options = {}) =>
  execFileSync(program, args, { encoding: "utf8", stdio: ["ignore", "pipe", "inherit"], ...options, env: { ...process.env, ...options.env } });
const quote = argument => (/[\s"]/.test(argument) ? `"${argument}"` : argument);
const pnpm = (args, options = {}) => {
  const common = { encoding: "utf8", stdio: ["ignore", "pipe", "inherit"], ...options, env: { ...process.env, ...options.env } };
  return process.platform === "win32" ? execSync(["pnpm.cmd", ...args.map(quote)].join(" "), common) : execFileSync("pnpm", args, common);
};

if (!tagged(tag)) {
  console.log(`round trip: skipped — ${tag} is not a tag in this checkout, so there is nothing published to install; it runs after the tag is pushed`);
  process.exit(0);
}
if (examples.length === 0) {
  console.error("no example under examples/; the round trip installs the getting-started, the same consumer the packed smoke does");
  process.exit(1);
}

const scratch = mkdtempSync(join(tmpdir(), "nightseam-roundtrip-"));
let server;
try {
  await roundTrip();
} catch (failure) {
  report(failure);
  throw failure;
} finally {
  if (server) stop(server);
  rmSync(scratch, { recursive: true, force: true, maxRetries: 10 });
}

async function roundTrip() {
  // A successful publish precedes registry propagation. Wait for the exact
  // release everywhere, then install once; failures still reach report().
  await waitForRegistries(tag, { log: step });
  const consumer = join(scratch, "consumer");
  step(`copying ${examples[0]} to a consumer outside the workspace`);
  copyRegistryConsumer(join(root, examples[0]), consumer);

  // Nothing is overridden and no proxy is laid: the manifest asks for the
  // version and the registry answers, or this is where the release is
  // found out.
  step(`pnpm install — @nightseam/* at ${version}, from npm`);
  pnpm(["install", "--no-frozen-lockfile"], { cwd: consumer, stdio: ["ignore", "inherit", "inherit"] });
  step("pnpm check");
  pnpm(["check"], { cwd: consumer, stdio: ["ignore", "inherit", "inherit"] });

  // The example carries no go.sum — the hash of a version nobody had
  // tagged was not knowable when it was written — so the consumer writes
  // one from what the proxy serves for the tag.
  const go = { GOWORK: "off", GOFLAGS: "-mod=mod" };
  step(`go get ${module}@${tag}`);
  run("go", ["get", `${module}@${tag}`], { cwd: consumer, env: go, stdio: ["ignore", "inherit", "inherit"] });
  run("go", ["mod", "download", "all"], { cwd: consumer, env: go, stdio: ["ignore", "inherit", "inherit"] });
  step("go tool nightseam check");
  run("go", ["tool", "nightseam", "check"], { cwd: consumer, env: go, stdio: ["ignore", "inherit", "inherit"] });
  adapter(go);

  const address = `127.0.0.1:${await free()}`;
  step(`go run ./server on ws://${address}/probe`);
  server = spawn("go", ["run", "./server"], {
    cwd: consumer,
    env: { ...process.env, ...go, PROBE_ADDRESS: address },
    stdio: ["ignore", "inherit", "inherit"],
    detached: process.platform !== "win32",
  });
  const deadline = Date.now() + 180_000;
  while (!(await listening(address))) {
    if (Date.now() > deadline || server.exitCode !== null) throw new Error(`the example's server never listened on ${address}`);
    await after(200);
  }

  step("pnpm start");
  const out = pnpm(["start"], { cwd: consumer, env: { PROBE_URL: `ws://${address}/probe` } });
  process.stdout.write(out);
  holdProbeExchange(out);
  console.log(`round trip: ${tag} installed from npm and from the proxy, and ${examples[0]} ran against it`);
}

/**
 * The OpenTelemetry adapter, which is a module of its own and released by a
 * second tag beside this one: it is the one release step that depends on
 * the first tag already being fetchable, so it is the one most worth
 * asking about here. A module of its own needs a consumer of its own,
 * since the example asks for none of it.
 */
function adapter(go) {
  const at = join(scratch, "adapter");
  mkdirSync(at);
  writeFileSync(join(at, "go.mod"), `module example.com/adapter\n\ngo ${directive()}\n`);
  writeFileSync(
    join(at, "main.go"),
    'package main\n\nimport otel "github.com/Bitspark/nightseam/otel/go"\n\nfunc main() { _ = otel.Propagator(nil) }\n',
  );
  step(`go get ${module}/otel/go@${tag}`);
  run("go", ["get", `${module}/otel/go@${tag}`], { cwd: at, env: go, stdio: ["ignore", "inherit", "inherit"] });
  run("go", ["build", "./..."], { cwd: at, env: go, stdio: ["ignore", "inherit", "inherit"] });
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
function report(failure) {
  if (!process.argv.includes("--open-issue")) return;
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
