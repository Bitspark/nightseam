import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { test } from "node:test";
import { root } from "./packages.mjs";
import { addPublishedDependencies, holdOutsiderImports } from "./smoke-imports.mjs";

function fixture(t) {
  const consumer = mkdtempSync(join(tmpdir(), "nightseam-imports-test-"));
  t.after(() => rmSync(consumer, { recursive: true, force: true, maxRetries: 10 }));
  const write = (path, value) => {
    const file = join(consumer, path);
    mkdirSync(dirname(file), { recursive: true });
    writeFileSync(file, value);
  };
  write("package.json", JSON.stringify({ name: "outsider", private: true, type: "module", dependencies: { "@nightseam/example": "0.5.0", unrelated: "^1.0.0" } }) + "\n");
  return { consumer, write };
}

const entry = file => ({ types: `./dist/${file}.d.ts`, import: `./dist/${file}.js` });
const manifests = [
  { name: "@nightseam/example", publishConfig: { exports: { ".": entry("index") } } },
  {
    name: "@nightseam/extra",
    exports: { ".": "./src/index.ts", "./conformance": "./src/conformance.ts" },
    publishConfig: { exports: { ".": entry("index"), "./nested": entry("nested") } },
  },
];

test("registry imports add every discovered package at the release version without replacing existing requirements", t => {
  const { consumer } = fixture(t);
  addPublishedDependencies(consumer, "0.5.0", manifests);
  const installed = JSON.parse(readFileSync(join(consumer, "package.json"), "utf8"));
  assert.deepEqual(installed.dependencies, { "@nightseam/example": "0.5.0", unrelated: "^1.0.0", "@nightseam/extra": "0.5.0" });
  assert.equal(installed.type, "module");
  assert.equal(installed.overrides, undefined);
});

test("a conflicting existing requirement fails without silently rewriting the consumer", t => {
  const { consumer } = fixture(t);
  const file = join(consumer, "package.json");
  const before = readFileSync(file);
  assert.throws(() => addPublishedDependencies(consumer, "0.6.0", manifests), /@nightseam\/example.*0\.5\.0.*0\.6\.0/);
  assert.deepEqual(readFileSync(file), before);
});

function installedFixture(t) {
  const result = fixture(t);
  for (const manifest of manifests) {
    const directory = `node_modules/${manifest.name}`;
    result.write(`${directory}/package.json`, JSON.stringify({ name: manifest.name, version: "0.5.0", type: "module", exports: manifest.publishConfig.exports }));
    for (const file of ["index", "nested"]) {
      result.write(`${directory}/dist/${file}.js`, "export const value = 1;\n");
      result.write(`${directory}/dist/${file}.d.ts`, "export declare const value: number;\n");
    }
  }
  // The consumer's own `@types/node`, a stub: the tsconfig the smoke writes
  // names the type library, and a junction to the workspace's copy fails
  // on the GitHub Windows runner, whose temp and checkout are on different
  // drives (#624). The fixture asks nothing of Node's types.
  result.write("node_modules/@types/node/package.json", JSON.stringify({ name: "@types/node", version: "0.0.0", types: "index.d.ts" }));
  result.write("node_modules/@types/node/index.d.ts", "// A stub for the fixture; the smoke's imports use nothing of it.\nexport {};\n");
  // A command that fails carries its output in the failure, so that a
  // runner's log says what tsc or node said and not only that it exited.
  const command = (args, options) => {
    try {
      return execFileSync(process.execPath, args, { ...options, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
    } catch (error) {
      error.message += `\n--- stdout ---\n${error.stdout ?? ""}\n--- stderr ---\n${error.stderr ?? ""}`;
      throw error;
    }
  };
  const logs = [];
  const runners = {
    pnpm: (args, options) => command([join(root, "node_modules/typescript/lib/tsc.js"), ...args.slice(2)], options),
    run: (_program, args, options) => { logs.push(command(args, options)); },
    step: message => logs.push(message),
  };
  return { ...result, runners, logs };
}

test("an outsider type-checks and loads an extra package's root and subpath without importing workspace helpers", t => {
  const { consumer, runners, logs } = installedFixture(t);
  holdOutsiderImports(consumer, runners, manifests);
  const output = logs.join("\n");
  assert.match(output, /loaded @nightseam\/example,/);
  assert.match(output, /loaded @nightseam\/extra,/);
  assert.match(output, /loaded @nightseam\/extra\/nested,/);
  assert.doesNotMatch(output, /conformance/);
});

test("an invalid published declaration in a package absent from the example fails the smoke", t => {
  const { consumer, write, runners } = installedFixture(t);
  write("node_modules/@nightseam/extra/dist/nested.d.ts", "export declare const value: MissingPublishedType;\n");
  assert.throws(() => holdOutsiderImports(consumer, runners, manifests), error => /MissingPublishedType/.test(error.stdout));
});

test("an undeclared runtime dependency in an extra published subpath fails the smoke", t => {
  const { consumer, write, runners } = installedFixture(t);
  write("node_modules/@nightseam/extra/dist/nested.js", 'import "missing-nightseam-fixture-dependency";\nexport const value = 1;\n');
  assert.throws(() => holdOutsiderImports(consumer, runners, manifests), error => /missing-nightseam-fixture-dependency/.test(error.stderr));
});
