// What `publishedEntryPoints` says a consumer may import, against manifests
// written here rather than read off the disk: the question is what the rule
// makes of a shape, and the shapes that matter include ones no package in
// this repository has yet. The packages' own manifests are held by the
// release smoke, which imports what this returns.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { packages, publishedEntryPoints, root } from "./packages.mjs";

test("a package with no exports map has the one entry point", () => {
  assert.deepEqual(publishedEntryPoints({ main: "./dist/index.js", types: "./dist/index.d.ts" }), [""]);
});

test("a bare exports string is the package itself", () => {
  assert.deepEqual(publishedEntryPoints({ exports: "./src/index.ts" }), [""]);
});

test("a map of conditions describes the root and nothing else", () => {
  const manifest = { exports: { types: "./dist/index.d.ts", import: "./dist/index.js", default: "./dist/index.js" } };
  assert.deepEqual(publishedEntryPoints(manifest), [""]);
});

test("publishConfig wins, so a workspace-only helper is not an entry point a consumer has", () => {
  // The shape duplex/ts and session/ts carry: `./conformance` is this
  // repository's fixture seam inside the workspace, and the published map
  // does not have it. A smoke that imported the workspace map would ask the
  // tarball for a path it was never meant to hold.
  const manifest = {
    exports: { ".": "./src/index.ts", "./conformance": "./src/conformance.ts" },
    publishConfig: { exports: { ".": { types: "./dist/index.d.ts", import: "./dist/index.js" }, "./package.json": "./package.json" } },
  };
  assert.deepEqual(publishedEntryPoints(manifest), [""]);
});

test("a published subpath is an entry point, and a manifest and a withheld one are not", () => {
  const manifest = {
    publishConfig: {
      exports: {
        ".": { import: "./dist/index.js" },
        "./live": { import: "./dist/live.js" },
        "./internal": null,
        "./package.json": "./package.json",
      },
    },
  };
  assert.deepEqual(publishedEntryPoints(manifest), ["", "/live"]);
});

test("every published package in this workspace names at least the package itself", () => {
  // Found rather than listed, the way the release finds them: a package
  // added beside the others is held here without being named.
  assert.ok(packages.length > 0);
  for (const directory of packages) {
    const manifest = JSON.parse(readFileSync(join(root, directory, "package.json"), "utf8"));
    const entries = publishedEntryPoints(manifest);
    assert.ok(entries.includes(""), `${directory} publishes no root entry point: ${JSON.stringify(entries)}`);
  }
});
