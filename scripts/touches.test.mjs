// What an issue's fields mean, against issue bodies of the shapes the tree
// actually holds — the form's `### Touches` heading and the hand-written bold
// `**Touches:**`. The parser knowing only the second made the scope note a
// no-op for every form-filed lane while still reporting success (#211).
import assert from "node:assert/strict";
import { test } from "node:test";
import { changelogNote, declared, named, scopeNote } from "./touches.mjs";

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

/** A body that declares the entry it will land, as the forms now ask. */
const declares = `### Held by

The existing gates.

### Changelog

The peer refuses a frame whose id it has already answered, in both languages.

### Touches

\`runtime/go\`, \`runtime/ts\`
`;

/** A body that declares there is nothing to record, and why. */
const declines = `### Changelog

None — it rewords a comment and changes nothing a consumer sees.

### Touches

\`runtime/go\`
`;

test("a body says whether it declared an entry, declined one, or has no field", () => {
  assert.equal(declared(declares), "entry");
  assert.equal(declared(declines), "none");
  assert.equal(declared("None of this is a Changelog field.\n"), "absent");
  assert.equal(declared(heading), "absent");
});

test("an issue that declared an entry and did not touch the changelog is noted, not refused", () => {
  const note = changelogNote({ number: 325, body: declares }, ["scripts/docs.mjs", "scripts/docs.test.mjs"]);
  assert.match(note, /^Changelog note/);
  assert.match(note, /not a refusal/);
  assert.match(note, /#325/);
});

test("an issue that declared an entry and touched the changelog is silent", () => {
  assert.equal(changelogNote({ number: 1, body: declares }, ["CHANGELOG.md", "runtime/go/peer.go"]), "");
});

test("an issue that declared None is silent however the reason is worded", () => {
  assert.equal(changelogNote({ number: 2, body: declines }, ["runtime/go/peer.go"]), "");
  assert.equal(changelogNote({ number: 3, body: "### Changelog\n\nNone.\n" }, ["runtime/go/peer.go"]), "");
});

test("an issue with no Changelog field is silent, unlike one naming no Touches", () => {
  // Every issue in the tree predates the field. A check that fired on all of
  // them would be turned off the day it landed.
  assert.equal(changelogNote({ number: 203, body: heading }, ["internal/model/expr.go"]), "");
  assert.equal(changelogNote({ number: 204, body: inline }, ["scripts/links.mjs"]), "");
});
