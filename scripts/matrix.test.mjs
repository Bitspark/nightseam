// What the matrix renders and what the tier table says of it. The fixtures
// here are matrices no run has produced yet — a tier 2 language lagging, a
// tier 3 one provisional — because the gate they exercise is what refuses a
// release, and a gate first exercised by the release it refuses is a gate
// nobody has run. docs/languages/tiers.md is what these hold.
import assert from "node:assert/strict";
import { test } from "node:test";
import { cell, columns, gate, missing, planned, table, unrun } from "./matrix.mjs";
import { replace, section, start, end } from "./matrix-table.mjs";

// The profiles as conformance/profiles.json declares them: the onboarding
// order, which is not the alphabetical order a matrix lists them in.
const profiles = {
  profiles: {
    core: { layers: ["seam", "peer"] },
    generator: { layers: ["generated"] },
    tunnel: { layers: ["tunnel"] },
    session: { layers: ["session"] },
    observability: { needs: ["observer", "propagator"] },
  },
  tiers: {
    1: { requires: ["core", "generator", "tunnel", "session", "observability"], onFailure: "stop" },
    2: { requires: ["core", "generator"], onFailure: "stop", otherwise: "stop-next" },
    3: { requires: ["core", "generator"], onFailure: "provisional" },
    4: { requires: ["core"], onFailure: "provisional" },
  },
  languages: { go: { tier: 1, reference: true }, typescript: { tier: 1 } },
  planned: { python: 2, rust: 2, csharp: 4 },
};

const green = { passed: 3, skipped: 0, failed: 0 };
const red = { passed: 1, skipped: 0, failed: 2 };

const row = (tier, cells, verdict = "ok") => ({ tier, verdict, cells });
const all = value => Object.fromEntries(Object.keys(profiles.profiles).map(name => [name, value]));

const matrix = {
  profiles: ["core", "generator", "observability", "session", "tunnel"],
  languages: { go: row(1, all(green)), typescript: row(1, all(green)) },
};

test("a cell says what passed, what was skipped with it, and what failed", () => {
  assert.equal(cell(undefined), "—");
  assert.equal(cell({ passed: 0, skipped: 0, failed: 0 }), "—");
  assert.equal(cell({ passed: 4, skipped: 0, failed: 0 }), "✓");
  assert.equal(cell({ passed: 4, skipped: 2, failed: 0 }), "✓ 2 skipped");
  assert.equal(cell({ passed: 0, skipped: 2, failed: 0 }), "— 2 skipped");
  // A failure is what a reader must see, whatever passed beside it.
  assert.equal(cell({ passed: 9, skipped: 1, failed: 3 }), "✗ 3 failed");
});

test("the columns are the order a language is built in, not the order the matrix lists", () => {
  assert.deepEqual(columns(matrix, profiles), ["core", "generator", "tunnel", "session", "observability"]);
});

test("a profile the matrix knows and the declaration does not keeps its column", () => {
  const extra = { ...matrix, profiles: [...matrix.profiles, "storage"] };
  assert.deepEqual(columns(extra, profiles).at(-1), "storage");
});

test("a matrix missing a language profiles.json places is named, since a filtered run writes one", () => {
  assert.deepEqual(missing(matrix, profiles), []);
  const partial = { ...matrix, languages: { typescript: matrix.languages.typescript } };
  assert.deepEqual(missing(partial, profiles), ["go"]);
});

test("a profile no row has a cell for is named, since a gate reads an absent cell as nothing failing", () => {
  assert.deepEqual(unrun(matrix, profiles), []);
  const withoutGenerator = {
    ...matrix,
    languages: Object.fromEntries(
      Object.entries(matrix.languages).map(([language, value]) => {
        const { generator, ...cells } = value.cells;
        return [language, { ...value, cells }];
      }),
    ),
  };
  assert.deepEqual(unrun(withoutGenerator, profiles), ["go has no generator cell", "typescript has no generator cell"]);
});

test("the table marks the reference and carries every language's tier and verdict", () => {
  const rendered = table(matrix, profiles);
  assert.match(rendered, /\| `go` \*\(reference\)\* \| 1 \|/);
  assert.match(rendered, /\| `typescript` \| 1 \|/);
  assert.doesNotMatch(rendered, /`typescript`.*reference/);
  // A row per language and two of header.
  assert.equal(rendered.split("\n").length, 4);
});

test("what is planned is named with its tier, so an absent language reads as coming and not as refused", () => {
  assert.equal(planned(profiles), "Planned, with no testee yet: `python`, `rust` at tier 2; `csharp` at tier 4.");
  assert.equal(planned({ ...profiles, planned: {} }), "");
});

test("the section is written between the markers and nothing outside them moves", () => {
  const document = `# Title\n\nbefore\n\n${start}\nstale\n${end}\n\nafter\n`;
  const written = replace(document, section(matrix, profiles));
  assert.match(written, /^# Title\n\nbefore\n/);
  assert.match(written, /\n\nafter\n$/);
  assert.doesNotMatch(written, /stale/);
  assert.match(written, /`go` \*\(reference\)\*/);
  // Rendering twice is rendering once: the check compares bytes.
  assert.equal(replace(written, section(matrix, profiles)), written);
});

test("a document without the markers is refused rather than given a section", () => {
  assert.throws(() => replace("# Title\n\nno markers\n", "x"), /no <!-- matrix:start -->/);
});

test("a tier 1 language failing anything stops the release", () => {
  const failing = { ...matrix, languages: { ...matrix.languages, typescript: row(1, { ...all(green), session: red }, "blocking") } };
  const { problems, provisional, lagging } = gate(failing, profiles, undefined);
  assert.equal(problems.length, 1);
  assert.match(problems[0], /`typescript` \(tier 1\) fails session, which tier 1 stops a release for/);
  assert.deepEqual(provisional, []);
  assert.deepEqual(lagging, []);
});

test("a tier 3 language failing what it guarantees ships, and is named provisional", () => {
  const failing = { profiles: matrix.profiles, languages: { java: row(3, { ...all(green), core: red }, "provisional") } };
  const { problems, provisional } = gate(failing, profiles, undefined);
  assert.deepEqual(problems, []);
  assert.equal(provisional.length, 1);
  assert.match(provisional[0], /`java` \(tier 3\) fails core; tier 3 ships provisional/);
});

test("a tier 4 language failing outside core is informational and says nothing", () => {
  const failing = { profiles: matrix.profiles, languages: { cpp: row(4, { ...all(green), session: red }) } };
  assert.deepEqual(gate(failing, profiles, undefined), { problems: [], provisional: [], lagging: [] });
});

test("a tier 2 language failing outside core and generator ships once and refuses the next release", () => {
  const failing = { profiles: matrix.profiles, languages: { python: row(2, { ...all(green), tunnel: red }) } };

  // No previous release: the lag begins, the release ships, and the notes say so.
  const first = gate(failing, profiles, undefined);
  assert.deepEqual(first.problems, []);
  assert.equal(first.lagging.length, 1);
  assert.match(first.lagging[0], /allows one release of lag, so the next release refuses it/);

  // The last release's matrix had it failing there too: this is the second.
  const second = gate(failing, profiles, failing);
  assert.deepEqual(second.lagging, []);
  assert.equal(second.problems.length, 1);
  assert.match(second.problems[0], /and did at the last release; tier 2 allows one release of lag and this is the second/);

  // A previous release it was green in starts the lag over.
  const wasGreen = { profiles: matrix.profiles, languages: { python: row(2, all(green)) } };
  assert.deepEqual(gate(failing, profiles, wasGreen).problems, []);
});

test("a tier 2 language failing what it guarantees stops the release however the lag stands", () => {
  const failing = { profiles: matrix.profiles, languages: { rust: row(2, { ...all(green), generator: red }) } };
  const { problems } = gate(failing, profiles, undefined);
  assert.equal(problems.length, 1);
  assert.match(problems[0], /fails generator, which tier 2 stops a release for/);
});

test("a language of no tier is in the matrix and gates nothing", () => {
  const entering = { profiles: matrix.profiles, languages: { haskell: { verdict: "provisional", cells: all(red) } } };
  assert.deepEqual(gate(entering, profiles, undefined), { problems: [], provisional: [], lagging: [] });
});

test("a green matrix gates nothing", () => {
  assert.deepEqual(gate(matrix, profiles, matrix), { problems: [], provisional: [], lagging: [] });
});
