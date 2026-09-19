// Every `## ` section of CHANGELOG.md carries each `### ` heading at most
// once.
//
// A version's section is what a release publishes: scripts/release-prepare.mjs
// slices the whole block into release-notes.md and release.yml hands that file
// to `gh release create --notes-file`. A heading appearing twice is therefore a
// release page with two *Removed* lists, and — the way it happened — the same
// removal stated twice in different words, because two lanes removed one thing
// for two good reasons and a heading is not a merge conflict.
//
// This runs on every pull request rather than at the tag, for the reason
// RELEASING.md gives for the packed smoke running there too: a duplicate
// introduced today would otherwise sit on `main` until the release, and by
// then the lane that wrote it is gone and nobody can say whether the two
// entries are one fact or two.
//
// It holds structure and not prose. Which sections a version has, what they
// say and what order they are in stays editorial; a heading appearing twice
// under one version is not.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { root } from "./packages.mjs";

/** Each `## ` section with the `### ` headings under it, in the order they appear. */
function sections() {
  const found = [];
  for (const line of readFileSync(join(root, "CHANGELOG.md"), "utf8").split(/\r?\n/)) {
    if (line.startsWith("## ")) found.push({ version: line.slice(3).trim(), headings: [] });
    else if (line.startsWith("### ") && found.length) found.at(-1).headings.push(line.slice(4).trim());
  }
  return found;
}

test("no version repeats a heading", () => {
  const repeated = [];
  for (const { version, headings } of sections()) {
    const seen = new Set();
    for (const heading of headings) {
      if (seen.has(heading)) repeated.push(`${version}: "${heading}"`);
      seen.add(heading);
    }
  }
  assert.deepEqual(
    repeated,
    [],
    `CHANGELOG.md repeats a heading under one version (${repeated.join("; ")}); the whole section becomes the release notes, so two lists of the same name read as two different claims`,
  );
});

test("the changelog has sections to hold, so an empty read is not a pass", () => {
  // A parser that stopped matching would report nothing repeated and say so by
  // passing, which is the one answer this must never give by accident.
  const found = sections();
  assert.ok(found.length > 1, `CHANGELOG.md parsed as ${found.length} section(s); the file has more than that`);
  assert.ok(
    found.some(section => section.headings.length > 0),
    "CHANGELOG.md parsed with no ### heading anywhere; the parser is not reading the file the release publishes",
  );
});
