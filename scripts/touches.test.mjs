// What an issue's Touches means, against issue bodies of both shapes the tree
// actually holds — the form's `### Touches` heading and the hand-written bold
// `**Touches:**`. The parser knowing only the second made the scope note a
// no-op for every form-filed lane while still reporting success (#211).
import assert from "node:assert/strict";
import { test } from "node:test";
import { named, scopeNote } from "./touches.mjs";

/** What `.github/ISSUE_TEMPLATE/lane.yml` renders, as #203 and #222 carry it. */
const heading = `### Provenance

#200 lane B.

### Waits on

nothing.

### Touches

\`session\`, \`internal/model\`, \`internal/check\`,
\`cmd/nightseam\`, \`docs\`

### Parity

Both languages, in this lane
`;

/** What a hand-written issue carries, as #105 and #172 do. */
const inline = `Some prose about the lane.

**Waits on** nothing. **Touches** \`scripts\`, \`.github/workflows\`
`;

test("a heading names its paths", () => {
  assert.deepEqual(named(heading), ["session", "internal/model", "internal/check", "cmd/nightseam", "docs"]);
});

test("a bold inline label names its paths", () => {
  assert.deepEqual(named(inline), ["scripts", ".github/workflows"]);
});

test("a heading's paths stop at the next heading", () => {
  // "Both languages, in this lane" is under Parity and is not a path; a
  // backticked word there must not be read as one.
  assert.ok(!named(heading).includes("Parity"));
  assert.deepEqual(named("### Touches\n\n`a`\n\n### Next\n\n`b`\n"), ["a"]);
});

test("an issue with no Touches names nothing", () => {
  assert.deepEqual(named("### What\n\nA design question.\n"), []);
  assert.deepEqual(named(undefined), []);
});

test("a trailing slash or glob is trimmed, as the paths are written loosely", () => {
  assert.deepEqual(named("**Touches:** `runtime/go/`, `internal/**`"), ["runtime/go", "internal"]);
});

test("a PR inside its Touches gets no note", () => {
  assert.equal(scopeNote({ number: 1, body: heading }, ["internal/model", "docs/wire"]), "");
});

test("a PR outside its Touches is named, and not refused", () => {
  const note = scopeNote({ number: 203, body: heading }, ["internal/model", "otel/go"]);
  assert.match(note, /not a refusal/);
  assert.match(note, /changes `otel\/go`/);
  assert.doesNotMatch(note, /`internal\/model`, which/);
});

test("an issue naming no Touches is said so rather than passing in silence", () => {
  // The failure #211 was: nothing named, nothing checked, nothing reported.
  const note = scopeNote({ number: 999, body: "### What\n\nno touches here\n" }, ["scripts", "docs"]);
  assert.match(note, /names no \*\*Touches\*\*/);
  assert.match(note, /held against nothing/);
});
