import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { test } from "node:test";
import { holdTarball, tar } from "./tarball.mjs";

const required = ["dist/index.js", "dist/index.d.ts", "README.md", "LICENSE", "NOTICE"];
for (const omitted of [undefined, ...required]) {
  test(`packed contents ${omitted ? `refuse a missing ${omitted}` : "include every required file"}`, t => {
    const scratch = mkdtempSync(join(tmpdir(), "nightseam-tarball-"));
    t.after(() => rmSync(scratch, { recursive: true, force: true }));
    for (const file of required.filter(file => file !== omitted)) {
      const path = join(scratch, "package", file);
      mkdirSync(dirname(path), { recursive: true });
      writeFileSync(path, "fixture\n");
    }
    const tarball = join(scratch, "fixture.tgz");
    // Written through the same helper that reads it: an absolute `-f` operand
    // is what GNU tar takes for a remote host, so neither end of this test
    // hands tar a path with a drive letter in it.
    tar(scratch, ["-czf", "fixture.tgz", "-C", ".", "package"]);
    if (omitted) assert.throws(() => holdTarball(tarball), error => error.message.includes(omitted));
    else assert.doesNotThrow(() => holdTarball(tarball));
  });
}
