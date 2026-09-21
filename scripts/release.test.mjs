// The release gate, rehearsed: scripts/release-prepare.mjs against fixture
// matrices. The tag is this checkout's own version, so that every other
// spelling the script holds — the manifests, the generator's constant, each
// nested module's requirement, the changelog section — passes and the matrix
// is the only thing under test. What the fixtures hold is the tier table of
// docs/languages/tiers.md, which no release has yet had occasion to apply.
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { root } from "./packages.mjs";

const script = fileURLToPath(new URL("./release-prepare.mjs", import.meta.url));
const scratch = mkdtempSync(join(tmpdir(), "nightseam-release-"));

/** This checkout's version, as the manifests spell it: the one tag that is ready here. */
const tag = "v" + JSON.parse(readFileSync(join(root, "runtime/ts/package.json"), "utf8")).version;

// A green matrix of the shape the suite writes. It is written here rather
// than read from conformance/matrix.json so that what the gate is given is
// the fixture and nothing else: the file on disk is the artifact of whatever
// run last touched the checkout, and a gate's test that moves with it holds
// the gate to nothing. Derive only the inventory and tier assignments from
// the declaration, so a new testee does not turn every fixture into a
// missing-language refusal. Every cell is deliberately green, including
// optional profiles: each test then introduces exactly its named defect.
const declaration = JSON.parse(readFileSync(join(root, "conformance/profiles.json"), "utf8"));
const profiles = Object.keys(declaration.profiles).sort();
const greenRow = tier => ({ tier, verdict: "ok", cells: Object.fromEntries(profiles.map(name => [name, { passed: 4, skipped: 0, failed: 0 }])) });
const green = {
  profiles,
  languages: Object.fromEntries(Object.entries(declaration.languages).map(([language, { tier }]) => [language, greenRow(tier)])),
};

const write = (name, matrix) => {
  const file = join(scratch, name);
  writeFileSync(file, JSON.stringify(matrix, null, 2));
  return file;
};

/** A matrix file with one cell of one language made red. */
function withFailure(name, language, tier, profile) {
  const row = green.languages[language] ?? greenRow(tier);
  const languages = { ...green.languages, [language]: { ...row, tier, cells: { ...row.cells, [profile]: { passed: 1, skipped: 0, failed: 2 } } } };
  return write(name, { ...green, languages });
}

/** Runs the script; returns what it said and how it exited, rather than throwing on a refusal. */
function prepare(...args) {
  try {
    return { code: 0, out: execFileSync("node", [script, tag, "--dry-run", ...args], { cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }) };
  } catch (error) {
    return { code: error.status, out: (error.stdout ?? "") + (error.stderr ?? "") };
  }
}

test("a green matrix releases, and a dry run writes nothing", () => {
  const { code, out } = prepare("--matrix", write("green.json", green), "--no-previous");
  assert.equal(code, 0, out);
  assert.match(out, /^ready: /m);
  assert.match(out, /the matrix holds against no previous release/);
  // A dry run writes nothing: no release notes, no licence copied.
  assert.match(out, /nothing written/);
  assert.match(out, /LICENSE and NOTICE were not copied/);
});

test("a tier 1 language failing a profile refuses the tag", () => {
  const { code, out } = prepare("--matrix", withFailure("tier1.json", "typescript", 1, "tunnel"), "--no-previous");
  assert.equal(code, 1);
  assert.match(out, /not ready to release/);
  assert.match(out, /`typescript` \(tier 1\) fails tunnel, which tier 1 stops a release for/);
});

test("a skipped required generated server role refuses the tag despite a stored ok verdict", () => {
  const incomplete = {
    ...green,
    languages: {
      ...green.languages,
      typescript: {
        ...green.languages.typescript,
        cells: { ...green.languages.typescript.cells, generator: { passed: 19, skipped: 1, failed: 0 } },
      },
    },
  };
  const { code, out } = prepare("--matrix", write("missing-generated-server.json", incomplete), "--no-previous");
  assert.equal(code, 1, out);
  assert.match(out, /not ready to release/);
  assert.match(out, /`typescript` \(tier 1\) skips generator, which tier 1 stops a release for/);
});

test("a tier 3 language failing what it guarantees ships, and the release names it", () => {
  const { code, out } = prepare("--matrix", withFailure("tier3.json", "java", 3, "core"), "--no-previous");
  assert.equal(code, 0, out);
  assert.match(out, /^ready: /m);
  assert.match(out, /`java` \(tier 3\) fails core; tier 3 ships provisional/);
  assert.match(out, /marking 1 language in the notes/);
});

test("a tier 2 language failing outside what it guarantees spends its lag, and the next release refuses it", () => {
  const lagging = withFailure("tier2.json", "python", 2, "tunnel");

  // The first release with that cell red: it ships, and says the next will not.
  const first = prepare("--matrix", lagging, "--no-previous");
  assert.equal(first.code, 0, first.out);
  assert.match(first.out, /tier 2 allows one release of lag, so the next release refuses it/);

  // The release after it, with the same cell red at the last one: refused.
  const second = prepare("--matrix", lagging, "--previous-matrix", lagging);
  assert.equal(second.code, 1);
  assert.match(second.out, /not ready to release/);
  assert.match(second.out, /and did at the last release; tier 2 allows one release of lag and this is the second/);

  // A release it was green at starts the lag over rather than ending it.
  const restarted = prepare("--matrix", lagging, "--previous-matrix", write("was-green.json", green));
  assert.equal(restarted.code, 0, restarted.out);
  assert.match(restarted.out, /so the next release refuses it/);
});

test("a matrix with a language missing refuses the tag, since a filtered run writes one", () => {
  const { go, ...languages } = green.languages;
  const partial = write("partial.json", { ...green, languages });
  const { code, out } = prepare("--matrix", partial, "--no-previous");
  assert.equal(code, 1);
  assert.match(out, /has no row for go, which profiles\.json places at a tier/);
});

test("a matrix with a profile nobody ran refuses the tag, whatever the cells it does carry say", () => {
  const languages = Object.fromEntries(
    Object.entries(green.languages).map(([language, row]) => {
      const { generator, ...cells } = row.cells;
      return [language, { ...row, cells }];
    }),
  );
  const { code, out } = prepare("--matrix", write("unrun.json", { ...green, languages }), "--no-previous");
  assert.equal(code, 1);
  assert.match(out, /go has no generator cell; a profile with no cell is one this run did not reach/);
});

test("a tag that is not a version is refused before anything is read", () => {
  try {
    execFileSync("node", [script, "0.3.0"], { cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
    assert.fail("a bare number was taken for a tag");
  } catch (error) {
    assert.equal(error.status, 2);
    assert.match(error.stderr, /usage: node scripts\/release-prepare\.mjs/);
  }
});
