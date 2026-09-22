// The profile table in docs/languages/tiers.md against the profiles
// conformance/profiles.json declares.
//
// RELEASING.md defers to that page for what a release refuses — "a release is
// a promise per language, and docs/languages/tiers.md says which promise" — so
// a profile the gate requires and the page does not name is a reader working
// out a tier's obligations from a list that is short. Nothing else holds the
// two together: scripts/matrix-table.mjs --check holds the README's Languages
// table to conformance/matrix.json, which is a different table against a
// different file.
//
// The page drifted in two steps and neither was a lane's mistake — the
// `session` row left with the profile it described, and the `live` profile
// arrived in a lane whose Touches did not include this page. A second copy of
// a list beside the data that declares it is a copy nobody updates, so the
// table is held here and the prose above it names no profile at all.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { root } from "./packages.mjs";

const page = join(root, "docs/languages/tiers.md");
const read = () => readFileSync(page, "utf8");
const profiles = () => JSON.parse(readFileSync(join(root, "conformance/profiles.json"), "utf8")).profiles;
const declared = () => Object.keys(profiles());

/** Every row of the table whose first column is the profile, in the order the page lists them: the cells, trimmed, with the profile's backticks removed. */
function rows() {
  const lines = read().split(/\r?\n/);
  const header = lines.findIndex(line => /^\|\s*profile\s*\|/i.test(line));
  assert.notEqual(header, -1, `${page} has no table whose first column is "profile"`);
  const out = [];
  // The header, then the separator, then a row per profile until the table ends.
  for (const line of lines.slice(header + 2)) {
    if (!line.startsWith("|")) break;
    const cells = line.split("|").slice(1, -1).map(cell => cell.trim());
    cells[0] = cells[0].replace(/`/g, "");
    out.push(cells);
  }
  return out;
}

/** The first cell of every row: the profiles, in the page's order. */
const tabled = () => rows().map(cells => cells[0]);

test("the tiers page names exactly the profiles the declaration does, in its order", () => {
  assert.deepEqual(
    tabled(),
    declared(),
    "docs/languages/tiers.md's profile table and conformance/profiles.json disagree; the page is what RELEASING.md sends a reader to for what a tier promises",
  );
});

test("the tiers page carries each profile's description as the data spells it", () => {
  // The description is the one sentence a consumer reads for what holding a
  // profile means, so the page carries the data's sentence and not a
  // paraphrase of it: a paraphrase is a second copy, and a second copy is
  // what rotted the profile list before this test existed.
  const described = profiles();
  for (const cells of rows()) {
    const [profile, description] = cells;
    assert.equal(
      description,
      described[profile]?.description,
      `docs/languages/tiers.md's row for ${profile} does not carry the description conformance/profiles.json declares; render the data's sentence, do not paraphrase it`,
    );
  }
});

test("every profile a tier requires has a row on the page", () => {
  const profiles = JSON.parse(readFileSync(join(root, "conformance/profiles.json"), "utf8"));
  const rows = new Set(tabled());
  for (const [tier, terms] of Object.entries(profiles.tiers ?? {})) {
    for (const profile of terms.requires ?? []) {
      assert.ok(rows.has(profile), `tier ${tier} requires ${profile}, which the tiers page's profile table does not name`);
    }
  }
});

test("the completeness promise names no profile, being defined over all of them", () => {
  // P3 is the one promise defined as *every* component, so a profile named in
  // its row is a second copy of the set — and that copy rotted: it read
  // "tunnel, observability" while `live` was a third profile promising the
  // same thing. The other promises are not defined over the set, and may say
  // "the generator" as ordinary prose. Words rather than a pattern, so a
  // profile spelled inside a longer word is not mistaken for a mention.
  const row = read()
    .split(/\r?\n/)
    .find(line => line.includes("P3"));
  assert.ok(row, "the tiers page has no P3 row");
  const words = new Set(row.toLowerCase().split(/[^a-z0-9]+/));
  const named = declared().filter(profile => words.has(profile));
  assert.deepEqual(named, [], `the P3 row names ${named.join(", ")}; it promises every profile, so let the table be the list`);
});
