import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, readdirSync, rmdirSync, unlinkSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";
import { root } from "./packages.mjs";

// A new test or conformance helper must reach the same check as shipped
// source. Exercise each package's actual command with files it has never
// seen, so naming only today's entry points cannot silently drop tomorrow's.
for (const component of readdirSync(root)) {
  const directory = join(root, component, "ts");
  if (!existsSync(join(directory, "package.json"))) continue;
  test(`${component}: check refuses type errors in new tests and conformance helpers`, () => {
    const probe = mkdtempSync(join(directory, "src", "typecheck-probe-"));
    const files = ["coverage.test.ts", "conformance.ts"].map(name => join(probe, name));
    try {
      for (const file of files) writeFileSync(file, "export const rejected: string = 42;\n");
      const result = spawnSync(process.execPath, ["--run", "check"], {
        cwd: directory,
        encoding: "utf8",
        timeout: 60_000,
      });
      assert.ifError(result.error);
      const output = result.stdout + result.stderr;
      assert.notEqual(result.status, 0, "check accepted ill-typed source");
      for (const name of ["coverage.test.ts", "conformance.ts"]) {
        assert.ok(output.includes(name), `check missed ${name}:\n${output}`);
      }
      assert.match(output, /Type 'number' is not assignable to type 'string'/);
    } finally {
      for (const file of files) unlinkSync(file);
      rmdirSync(probe);
    }
  });
}
