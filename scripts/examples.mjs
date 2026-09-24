import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, dirname, join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { copyNotices, copyRegistryConsumer, root } from "./packages.mjs";
import { prepareGoRehearsal } from "./rehearsal.mjs";

const usage = "usage: node scripts/examples.mjs list | run <example> [--language=go|ts] [--keep] | check --all [--keep]";
const read = path => JSON.parse(readFileSync(path, "utf8"));

export function catalogue() {
  return readdirSync(join(root, "examples"), { withFileTypes: true })
    .filter(entry => entry.isDirectory())
    .map(entry => {
      const id = entry.name;
      assert.match(id, /^[a-z][a-z0-9-]*$/);
      const directory = join(root, "examples", id);
      const manifest = read(join(directory, "example.json"));
      assert.ok(["source", "released"].includes(manifest.availability), id);
      assert.ok(["wire", "probe"].includes(manifest.runner), id);
      assert.deepEqual(manifest.languages, ["go", "ts"], id);
      assert.equal(typeof manifest.title, "string", id);
      for (const file of ["README.md", "go.mod", "package.json"]) assert.ok(existsSync(join(directory, file)), `${id}: missing ${file}`);
      if (manifest.runner === "wire") {
        for (const file of ["go/main.go", "ts/main.ts", "tsconfig.json", "expected.txt"]) assert.ok(existsSync(join(directory, file)), `${id}: missing ${file}`);
        const documented = readFileSync(join(directory, "README.md"), "utf8").match(/```text\r?\n([\s\S]*?)```/);
        assert.ok(documented, `${id}: README must show the expected output`);
        holdOutput(documented[1], readFileSync(join(directory, "expected.txt"), "utf8"), `${id}/README`);
      }
      return { id, directory, ...manifest };
    })
    .sort((a, b) => a.order - b.order || a.id.localeCompare(b.id));
}

export function selectExamples(args, all = catalogue()) {
  const keep = args.includes("--keep");
  const options = args.filter(arg => arg.startsWith("--"));
  assert.equal(new Set(options).size, options.length, usage);
  const language = args.find(arg => arg.startsWith("--language="))?.slice("--language=".length);
  if (language !== undefined && !["go", "ts"].includes(language)) throw new Error("language must be go or ts");
  if (args.length === 1 && args[0] === "list") return { examples: all, list: true, keep };
  if (args[0] === "check" && args[1] === "--all" && args.length === (keep ? 3 : 2) && args.every(arg => ["check", "--all", "--keep"].includes(arg))) {
    return { examples: all, languages: ["go", "ts"], keep };
  }
  if (args[0] !== "run" || !args[1] || args.slice(2).some(arg => arg !== "--keep" && arg !== `--language=${language}`)) throw new Error(usage);
  const example = all.find(item => item.id === args[1]);
  if (!example) throw new Error(`unknown example: ${args[1]}`);
  if (example.runner === "probe" && language) throw new Error("probe uses both languages; omit --language");
  return { examples: [example], languages: language ? [language] : ["go", "ts"], keep };
}

export function holdOutput(actual, expected, label) {
  const normalize = text => text.replaceAll("\r\n", "\n");
  assert.equal(normalize(actual), normalize(expected), `${label}: observed output differs from expected.txt`);
}

function run(program, args, options = {}) {
  try {
    return execFileSync(program, args, { cwd: root, encoding: "utf8", windowsHide: true, timeout: 600000,
      maxBuffer: 16 << 20, stdio: ["ignore", "pipe", "pipe"], ...options,
      env: { ...process.env, ...options.env } });
  } catch (error) {
    process.stderr.write(error.stdout ?? "");
    process.stderr.write(error.stderr ?? "");
    throw error;
  }
}

function pnpm(args, options = {}) {
  if (process.platform !== "win32") return run("pnpm", args, options);
  const shims = run("where.exe", ["pnpm"]).trim().split(/\r?\n/);
  for (const shim of shims) {
    if (shim.toLowerCase().endsWith(".exe")) return run(shim, args, options);
    // npm global/Corepack shims and the local .bin shim used by CI installers.
    // Resolve the executable directly so paths and arguments never enter cmd.exe.
    for (const relative of [
      "pnpm.exe", "node_modules/pnpm/pnpm.exe", "../pnpm/pnpm.exe",
      "node_modules/@pnpm/exe/pnpm.exe", "../@pnpm/exe/pnpm.exe",
      "node_modules/pnpm/bin/pnpm.cjs", "../pnpm/bin/pnpm.cjs",
      "node_modules/corepack/dist/pnpm.js", "../corepack/dist/pnpm.js",
    ]) {
      const target = resolve(dirname(shim), relative);
      if (existsSync(target)) return target.endsWith(".exe")
        ? run(target, args, options)
        : run(process.execPath, [target, ...args], options);
    }
  }
  throw new Error("Cannot find pnpm beside its Windows shim.");
}

async function main(args) {
  const selection = selectExamples(args);
  if (selection.list) {
    for (const example of selection.examples) {
      const availability = example.availability === "source" ? "unreleased source" : "also available in released tags";
      console.log(`${example.id} — ${example.title} [${availability}; Go + TypeScript]`);
    }
    return;
  }
  console.log(`Preparing packaged examples from the working tree based on ${run("git", ["rev-parse", "--short", "HEAD"]).trim()}.`);
  console.log("Installing locked workspace dependencies and building packages.");
  pnpm(["install", "--frozen-lockfile"]);
  pnpm(["-r", "build"]);
  copyNotices();
  const scratch = mkdtempSync(join(tmpdir(), "nightseam-examples-"));
  try {
    const packed = {};
    const tarballs = join(scratch, "tarballs");
    mkdirSync(tarballs);
    if (selection.languages.includes("ts") && selection.examples.some(example => example.runner === "wire")) {
      for (const component of ["duplex", "runtime"]) {
        const directory = join(root, component, "ts");
        const manifest = read(join(directory, "package.json"));
        pnpm(["pack", "--pack-destination", tarballs], { cwd: directory });
        const file = `${manifest.name.replace("@", "").replace("/", "-")}-${manifest.version}.tgz`;
        assert.ok(existsSync(join(tarballs, file)), file);
        packed[manifest.name] = join(tarballs, file).replaceAll("\\", "/");
      }
    }
    for (const example of selection.examples) {
      console.log(`Running ${example.id}.`);
      if (example.runner === "probe") {
        process.stdout.write(run(process.execPath, ["scripts/smoke-packed.mjs", ...(selection.keep ? ["--keep"] : [])]));
        console.log("PASS probe: Go server and TypeScript client over WebSockets");
        continue;
      }
      const consumer = join(scratch, example.id);
      copyRegistryConsumer(example.directory, consumer);
      const expected = readFileSync(join(consumer, "expected.txt"), "utf8");
      for (const language of selection.languages) {
        let actual;
        if (language === "go") {
          const version = read(join(root, "runtime/ts/package.json")).version;
          const rehearsal = prepareGoRehearsal({ root, scratch, consumer, module: "github.com/Bitspark/nightseam", version });
          actual = run("go", ["run", "./go"], { cwd: consumer, env: rehearsal.env });
        } else {
          writeFileSync(join(consumer, "pnpm-workspace.yaml"), "packages: []\noverrides:\n" +
            Object.entries(packed).map(([name, file]) => `  "${name}": ${JSON.stringify("file:" + file)}`).join("\n") + "\n");
          pnpm(["install", "--no-frozen-lockfile"], { cwd: consumer });
          pnpm(["check"], { cwd: consumer });
          actual = run(process.execPath, ["--experimental-strip-types", "--disable-warning=ExperimentalWarning", "ts/main.ts"], { cwd: consumer });
        }
        holdOutput(actual, expected, `${example.id}/${language}`);
        process.stdout.write(actual);
        console.log(`PASS ${example.id}/${language}`);
      }
    }
  } finally {
    if (selection.keep) console.log(`Consumer copies retained at ${scratch}`);
    else {
      assert.equal(dirname(resolve(scratch)), resolve(tmpdir()));
      assert.ok(basename(scratch).startsWith("nightseam-examples-"));
      rmSync(scratch, { recursive: true, force: true, maxRetries: 10 });
    }
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  main(process.argv.slice(2)).catch(error => { console.error(error); process.exitCode = 1; });
}
