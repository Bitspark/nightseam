// The outsider's install, before a tag exists: every published package is
// packed, the getting-started example is copied out of the workspace, and
// what it resolves is the packed shape and nothing else — no `workspace:*`
// link, no `replace` to this checkout. Then it is built, run and read.
//
//	node scripts/smoke-packed.mjs             # pack, install, build, run
//	node scripts/smoke-packed.mjs --keep      # and leave the scratch directory
//
// It answers the one question every other gate leaves open: whether what is
// published can be installed and used. A `files` field that omits `dist`, an
// `exports` entry naming a path the tarball does not hold, a dependency a
// workspace link satisfied and a registry would not, a Go package that only
// ever resolved through a sibling checkout — each passes every test in this
// repository and fails at a consumer's install, which is after the tag.
// RELEASING.md says where it runs and what it still does not answer.
//
// It needs no registry and no tag. npm is answered by the tarballs, through
// a pnpm `overrides` map to their paths rather than a bare
// `pnpm add ./*.tgz`: the published packages depend on each other, and an
// override reaches a transitive `@nightseam/duplex` where a direct install
// would go looking for it on the registry. Go is answered by a file://
// module proxy laid from the tree — a module zip is content-addressed, so
// one written here is what the proxy would serve for the tag, and nothing
// has to be tagged, pushed or cleaned up to produce it.
import { cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { execFileSync, execSync, spawn } from "node:child_process";
import { connect, createServer } from "node:net";
import { setTimeout as after } from "node:timers/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { examples, packages, publishedEntryPoints, root } from "./packages.mjs";
import { holdTarball } from "./tarball.mjs";
import { prepareGoRehearsal } from "./rehearsal.mjs";

const keep = process.argv.includes("--keep");
const manifest = directory => JSON.parse(readFileSync(join(directory, "package.json"), "utf8"));
const version = manifest(join(root, packages[0])).version;
const module = "github.com/Bitspark/nightseam";
const step = message => console.log("smoke:", message);
const run = (program, args, options = {}) =>
  execFileSync(program, args, { encoding: "utf8", stdio: ["ignore", "pipe", "inherit"], ...options, env: { ...process.env, ...options.env } });
// On Windows pnpm is a .cmd, which Node refuses to spawn as a file and
// runs as a command line instead.
const quote = argument => (/[\s"]/.test(argument) ? `"${argument}"` : argument);
const pnpm = (args, options = {}) => {
  const common = { encoding: "utf8", stdio: ["ignore", "pipe", "inherit"], ...options, env: { ...process.env, ...options.env } };
  return process.platform === "win32"
    ? execSync(["pnpm.cmd", ...args.map(quote)].join(" "), common)
    : execFileSync("pnpm", args, common);
};

// The tarball holds `dist` and nothing else of the source, so a tree that
// has not been built packs an empty package and every failure below reads
// as something it is not.
const unbuilt = packages.filter(directory => !existsSync(join(root, directory, "dist", "index.js")));
if (unbuilt.length) {
  console.error(`not built: ${unbuilt.join(", ")}\nrun pnpm -r build first; what the tarballs hold is dist`);
  process.exit(1);
}
if (examples.length === 0) {
  console.error("no example under examples/; the smoke's consumer is the getting-started, and there is one consumer for both smokes");
  process.exit(1);
}

const scratch = mkdtempSync(join(tmpdir(), "nightseam-smoke-"));
let server;
try {
  await smoke();
} finally {
  if (server) stop(server);
  if (keep) console.log("smoke: kept", scratch);
  else rmSync(scratch, { recursive: true, force: true, maxRetries: 10 });
}

async function smoke() {
  // Every published package, packed the way the release publishes it:
  // publishConfig swaps the entry points to `dist` and the `workspace:*`
  // dependencies are rewritten to the version, both of which pnpm does
  // here and neither of which any other test sees. They are packed one by
  // one rather than with `pnpm -r pack`, which would pack the conformance
  // testee beside them: what is published is what scripts/packages.mjs
  // finds, and that is the list the release moves.
  const tarballs = join(scratch, "tarballs");
  mkdirSync(tarballs);
  const packed = {};
  for (const directory of packages) {
    const name = manifest(join(root, directory)).name;
    step(`packing ${name} from ${directory}`);
    pnpm(["pack", "--pack-destination", tarballs], { cwd: join(root, directory) });
    const file = readdirSync(tarballs).find(candidate => candidate === `${name.replace("@", "").replace("/", "-")}-${version}.tgz`);
    if (!file) throw new Error(`${name} packed no tarball for ${version}; the directory holds ${readdirSync(tarballs).join(", ") || "nothing"}`);
    holdTarball(join(tarballs, file));
    packed[name] = file;
  }

  // The example, outside the workspace: a copy, because in the tree pnpm
  // would read the root workspace and go would read the root module, and
  // resolving through either is the thing being ruled out.
  const consumer = join(scratch, "consumer");
  step(`copying ${examples[0]} to a consumer outside the workspace`);
  cpSync(join(root, examples[0]), consumer, { recursive: true, filter: source => !/[\\/](node_modules|dist)$/.test(source) });
  mkdirSync(join(consumer, "tarballs"));
  for (const file of Object.values(packed)) cpSync(join(tarballs, file), join(consumer, "tarballs", file));
  // The example depends on the two packages a generated client needs, and a
  // tarball nobody installs is a tarball nobody checked, so every other
  // package the release publishes is added to the copy.
  const consumed = manifest(consumer);
  for (const name of Object.keys(packed)) consumed.dependencies[name] ??= version;
  writeFileSync(join(consumer, "package.json"), JSON.stringify(consumed, null, 2) + "\n");
  // Since pnpm 10 an override is a workspace setting rather than a manifest
  // field, so the copy is given a workspace of one project to carry them.
  // They are written into the copy and never into the checkout: what is
  // committed names the versions a consumer of the registry names, and the
  // overrides are what stands in for the registry this once.
  writeFileSync(
    join(consumer, "pnpm-workspace.yaml"),
    "packages: []\noverrides:\n" + Object.entries(packed).map(([name, file]) => `  "${name}": "file:./tarballs/${file}"`).join("\n") + "\n",
  );

  step("pnpm install, every @nightseam package overridden to its tarball");
  pnpm(["install", "--no-frozen-lockfile"], { cwd: consumer, stdio: ["ignore", "inherit", "inherit"] });
  holdEntryPoints(consumer, Object.keys(packed));

  step("pnpm check");
  pnpm(["check"], { cwd: consumer, stdio: ["ignore", "inherit", "inherit"] });

  holdOutsiderImports(consumer);

  // Go, from a proxy carrying one module: what is not Nightseam falls
  // through to whatever GOPROXY the machine already has, and Nightseam
  // itself is served from here or from nowhere.
  const rehearsal = prepareGoRehearsal({ root, scratch, consumer, module, version });
  const go = rehearsal.env;
  step(`laid a file:// module proxy for ${module}@${rehearsal.version}, with an isolated module cache`);
  step("go mod download all");
  run("go", ["mod", "download", "all"], { cwd: consumer, env: go, stdio: ["ignore", "inherit", "inherit"] });
  // The generator the example names as a tool, built from the packed
  // module inside the consumer's own: the published tool saying that the
  // published example's checked-in output is what it renders.
  step("go tool nightseam check");
  run("go", ["tool", "nightseam", "check"], { cwd: consumer, env: go, stdio: ["ignore", "inherit", "inherit"] });

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
  // What the exchange is, one line per level the release publishes: the call
  // and the reverse call the server makes inside it, the event it emits before
  // either returns — and then the live pair, a reference handed over inside a
  // request and a reference handed back out of it. The last line is the one
  // that could not be produced by data or RPC alone: it is the server calling
  // the client's `notice` from inside the `stop` the client was given, long
  // after the call that carried either of them returned. A generated live
  // surface that is installed and never called is a surface nobody checked.
  for (const line of ["echo    -> olleh", "changed -> hello", "notice  -> demo", "stopped -> demo done"]) {
    if (!out.includes(line)) throw new Error(`the example printed no ${JSON.stringify(line)}:\n${out}`);
  }
  console.log(`smoke: ${Object.keys(packed).length} packages and ${module}@${rehearsal.version} installed from outside the workspace, and ${examples[0]} ran against them`);
}

/**
 * Every published package, imported from outside the workspace at every
 * entry point it publishes: first type-checked against the declarations the
 * tarball carries, then actually loaded by Node.
 *
 * The example imports two of the packages the release publishes, so without
 * this the rest are packed, installed, held to having the files they name,
 * and never opened. What that leaves unasked is everything only a real
 * import answers: a `dist` that imports a package the workspace link
 * satisfied and the manifest does not declare, an `exports` condition that
 * resolves to nothing under Node's own resolver, declarations that only ever
 * type-checked against sibling *sources* rather than against a sibling's
 * emitted `.d.ts`, an entry point the build emitted nothing into. Each of
 * those passes every other gate here and is given at a consumer's install.
 *
 * It is generic over what packages.mjs finds and what each manifest
 * publishes, so a package added beside the others — or a subpath one of them
 * begins publishing — is held by it without being named here.
 */
function holdOutsiderImports(consumer) {
  const specifiers = packages.flatMap(directory => {
    const declared = manifest(join(root, directory));
    return publishedEntryPoints(declared).map(entry => declared.name + entry);
  });
  const directory = join(consumer, "nightseam-imports");
  mkdirSync(directory, { recursive: true });
  const note = "// Written by scripts/smoke-packed.mjs into the copied consumer, never into the checkout.\n";
  writeFileSync(
    join(directory, "imports.ts"),
    note +
      specifiers.map((specifier, at) => `import * as module${at} from ${JSON.stringify(specifier)};`).join("\n") +
      `\n\nexport const imported: readonly unknown[] = [${specifiers.map((_, at) => `module${at}`).join(", ")}];\n`,
  );
  // The consumer's own tsconfig checks the example's sources with
  // skipLibCheck, which is what an application wants. This asks the opposite
  // question — whether the declarations the release publishes are sound when
  // a stranger compiles against them — so library checking is on.
  writeFileSync(
    join(directory, "tsconfig.json"),
    JSON.stringify(
      {
        compilerOptions: {
          target: "ES2022",
          module: "NodeNext",
          moduleResolution: "NodeNext",
          lib: ["ES2022", "DOM"],
          strict: true,
          skipLibCheck: false,
          types: ["node"],
          noEmit: true,
        },
        include: ["imports.ts"],
      },
      null,
      2,
    ) + "\n",
  );
  // A namespace with nothing in it is an entry point the build emitted
  // nothing into, which reads downstream as the package not having the export
  // somebody wanted rather than as a tarball nobody filled.
  writeFileSync(
    join(directory, "load.mjs"),
    note +
      `const specifiers = ${JSON.stringify(specifiers, null, 2)};\n` +
      `for (const specifier of specifiers) {\n` +
      `  const loaded = await import(specifier);\n` +
      `  const exported = Object.keys(loaded).filter(name => name !== "default");\n` +
      `  if (exported.length === 0) throw new Error(specifier + " loaded and exported nothing; its entry point in the tarball is empty");\n` +
      `  console.log("  loaded " + specifier + ", " + exported.length + " exports");\n` +
      `}\n`,
  );
  step(`tsc over an import of all ${specifiers.length} published entry points, with library checking on`);
  pnpm(["exec", "tsc", "-p", "nightseam-imports/tsconfig.json"], { cwd: consumer, stdio: ["ignore", "inherit", "inherit"] });
  step("node, loading each of them from the installed tarballs");
  run("node", ["nightseam-imports/load.mjs"], { cwd: consumer, stdio: ["ignore", "inherit", "inherit"] });
}

/**
 * Every entry point the installed manifests declare is a file the tarball
 * holds. A `files` field that omits `dist` and an `exports` entry naming a
 * path that was never packed both survive the install and fail somewhere
 * downstream of it; here each fails at once, with the package named.
 */
function holdEntryPoints(consumer, expected) {
  const scope = join(consumer, "node_modules", "@nightseam");
  const installed = existsSync(scope) ? readdirSync(scope) : [];
  const missing = expected
    .filter(name => !installed.includes(name.slice("@nightseam/".length)))
    .map(name => `${name}: packed, and the install put it nowhere under ${scope}`);
  for (const name of installed) {
    const directory = join(scope, name);
    for (const [field, entry] of entryPoints(manifest(directory))) {
      if (!existsSync(join(directory, entry))) missing.push(`@nightseam/${name}: ${field} names ${entry}, which its tarball does not hold`);
    }
  }
  if (missing.length) throw new Error("the packed shape is incomplete:\n  " + missing.join("\n  "));
  step(`${installed.length} @nightseam packages installed; every entry point they declare is in the tarball`);
}

/** Every relative path a manifest names as an entry point, with the field that names it. */
function* entryPoints(declared) {
  for (const field of ["main", "module", "types", "typings"]) {
    if (typeof declared[field] === "string") yield [field, declared[field]];
  }
  yield* (function* walk(value, path) {
    if (typeof value === "string") yield [path, value];
    else if (value && typeof value === "object") for (const [key, nested] of Object.entries(value)) yield* walk(nested, `${path}.${key}`);
  })(declared.exports, "exports");
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
