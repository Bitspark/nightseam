// What the documentation check reads as coverage and as the decision form, on
// pages no tree has — an index that lists all but one of a directory, a page
// reached only by an absolute URL of this repository, a decision missing its
// serves, a built-in family whose reference the index beside it never grew a
// row for — because a check first exercised by the page that slips past it is a
// check nobody has run.
import assert from "node:assert/strict";
import { test } from "node:test";
import { covered, driverInventory, driverOps, duplicated, formless, pagesOf, parts, referencesOf, sets, uncovered, unlisted } from "./docs.mjs";

/** The five parts, as a page in the form spells them. */
const whole = [
  "# A decision",
  "",
  "**The question.** What?",
  "",
  "**Decided.** This.",
  "",
  "**Why.** The alternative cost more.",
  "",
  "**Serves.** Agnosticism.",
  "",
  "**Since.** 0.5.0, #1.",
  "",
].join("\n");

test("an index covers a page by any link that resolves to it", () => {
  const index = [
    "| [families](declaration/families.md) | the tier files |",
    "| [generics](declaration/generics.md#apply-and-with) | an anchor still covers the page |",
    "| [pipeline](https://github.com/Bitspark/nightseam/blob/main/docs/declaration/pipeline.md) | this repository's own URL, as a package README spells it |",
    "Not a link: `[generated](declaration/generated.md)`, and <https://example.test/> is somebody else's.",
  ].join("\n");
  assert.deepEqual(
    [...covered(index, "docs/README.md")].sort(),
    ["docs/declaration/families.md", "docs/declaration/generics.md", "docs/declaration/pipeline.md"],
  );
});

test("a set's pages are its own Markdown, without its index or a directory below it", () => {
  const tracked = new Set([
    "docs/decisions/README.md",
    "docs/decisions/one-reference-form.md",
    "docs/decisions/a-union-is-adjacently-tagged.md",
    "docs/decisions/art/diagram.svg",
    "docs/decisions/nested/README.md",
    "docs/runtime/peer.md",
  ]);
  assert.deepEqual(pagesOf("docs/decisions", tracked), [
    "docs/decisions/a-union-is-adjacently-tagged.md",
    "docs/decisions/one-reference-form.md",
  ]);
});

test("a page its index does not link to is named, and the rest of the set is not", () => {
  const tracked = new Set(["docs/goals/README.md", "docs/goals/boundary.md", "docs/goals/layering.md"]);
  const pages = new Map([
    ["docs/goals/README.md", "| [boundary](boundary.md) | the one it lists |"],
    ["docs/goals/boundary.md", "# Boundary\n"],
    ["docs/goals/layering.md", "# Layering\n"],
  ]);
  const problems = uncovered(pages, tracked).filter(p => p.page.startsWith("docs/goals/"));
  assert.deepEqual(problems, [
    { page: "docs/goals/layering.md", reason: "is under docs/goals and docs/goals/README.md does not link to it" },
  ]);
});

test("a page its index lists twice is named, and one listed once is not", () => {
  const tracked = new Set(["docs/goals/README.md", "docs/goals/boundary.md", "docs/goals/layering.md"]);
  const pages = new Map([
    [
      "docs/goals/README.md",
      [
        "| [boundary](boundary.md) | once |",
        "| [boundary](boundary.md#at-the-limit) | and again, as a merge of two lanes leaves it |",
        "| [layering](layering.md) | once |",
      ].join("\n"),
    ],
    ["docs/goals/boundary.md", "# Boundary\n"],
    ["docs/goals/layering.md", "# Layering\n"],
  ]);
  assert.deepEqual(duplicated(pages, tracked).filter(p => p.page.startsWith("docs/goals/")), [
    { page: "docs/goals/boundary.md", reason: "is listed 2 times by docs/goals/README.md" },
  ]);
  assert.deepEqual(uncovered(pages, tracked).filter(p => p.page.startsWith("docs/goals/")), []);
});

test("a decision missing a part is named by the parts it is missing", () => {
  const tracked = new Set(["docs/decisions/whole.md", "docs/decisions/partial.md", "docs/decisions/none.md"]);
  const pages = new Map([
    ["docs/decisions/whole.md", whole],
    ["docs/decisions/partial.md", whole.replace("**Serves.** Agnosticism.\n\n", "")],
    ["docs/decisions/none.md", "# A page that records nothing\n\nProse alone.\n"],
  ]);
  assert.deepEqual(formless(pages, tracked), [
    { page: "docs/decisions/none.md", reason: `is a decision without its ${parts.join(", ")}` },
    { page: "docs/decisions/partial.md", reason: "is a decision without its Serves" },
  ]);
});

test("a page recording two verdicts says which why it is, and still has one", () => {
  const tracked = new Set(["docs/decisions/two.md"]);
  const two = whole.replace("**Why.**", "**Why this replaces the first verdict.**");
  assert.deepEqual(formless(new Map([["docs/decisions/two.md", two]]), tracked), []);
});

test("every set names an index inside the tree it indexes", () => {
  for (const { directory, index } of sets) {
    assert.ok(directory.startsWith("docs/"), `${directory} is not documentation`);
    assert.ok(index.endsWith("/README.md"), `${index} is not a README`);
    assert.ok(directory.startsWith(index.slice(0, index.lastIndexOf("/"))), `${index} is not above ${directory}`);
  }
});

/** The built-in references as the tree holds them: a directory per family. */
const references = new Set([
  "docs/declaration/builtins/README.md",
  "docs/declaration/builtins/duplex/README.md",
  "docs/declaration/builtins/live/README.md",
  "docs/declaration/builtins/tunnel/README.md",
  "docs/declaration/builtins/duplex/diagram.svg",
  "docs/declaration/families.md",
]);

test("a built-in reference is a directory's README, which a set's pages never are", () => {
  assert.deepEqual(referencesOf("docs/declaration/builtins", references), [
    "docs/declaration/builtins/duplex/README.md",
    "docs/declaration/builtins/live/README.md",
    "docs/declaration/builtins/tunnel/README.md",
  ]);
  assert.deepEqual(pagesOf("docs/declaration/builtins", references), []);
});

test("a built-in its index does not list is named, and the ones it lists are not", () => {
  const pages = new Map([
    [
      "docs/declaration/builtins/README.md",
      [
        "- [duplex](duplex/README.md): the profile's envelope and channel handle.",
        "- [tunnel](tunnel/README.md): opening channels, credit, and their wire types.",
      ].join("\n"),
    ],
  ]);
  assert.deepEqual(unlisted(pages, references), [
    {
      page: "docs/declaration/builtins/live/README.md",
      reason: "is a built-in family reference and docs/declaration/builtins/README.md does not link to it",
    },
  ]);
});

test("an index listing every built-in satisfies the claim", () => {
  const pages = new Map([
    [
      "docs/declaration/builtins/README.md",
      ["- [duplex](duplex/README.md)", "- [live](live/README.md)", "- [tunnel](tunnel/README.md)"].join("\n"),
    ],
  ]);
  assert.deepEqual(unlisted(pages, references), []);
});

/** The driver documents operations in the first column of its op tables. */
const driver = (...ops) => [
  "| op | arguments | answer |",
  "| --- | --- | --- |",
  ...ops.map(op => `| \`${op}\` | none | {} |`),
].join("\n");
const scenario = (...ops) => ({ steps: ops.map(op => ({ on: "a", op })) });

test("a driven op without a driver row is refused by name and scenario", () => {
  const { problems, notes } = driverInventory(driver("peer.call"), new Map([
    ["conformance/scenarios/peer/missing.json", scenario("peer.call", "peer.forgotten", "peer.forgotten")],
  ]));
  assert.deepEqual(problems, [{
    page: "conformance/scenarios/peer/missing.json",
    reason: "drives peer.forgotten, which has no op row in conformance/DRIVER.md",
  }]);
  assert.deepEqual(notes, []);
});

test("a documented but undriven op is a note and never a refusal", () => {
  assert.deepEqual(driverInventory(driver("peer.call", "peer.future"), new Map([
    ["conformance/scenarios/peer/call.json", scenario("peer.call")],
  ])), {
    problems: [],
    notes: [{ page: "conformance/DRIVER.md", reason: "documents peer.future, which no scenario drives" }],
  });
});

test("every driven op with a row satisfies the inventory, including runner ops", () => {
  assert.deepEqual(driverInventory(driver("peer.call", "pair.peers"), new Map([
    ["conformance/scenarios/peer/call.json", { steps: [
      { on: "runner", op: "pair.peers" },
      { on: "a", op: "peer.call", repeat: { max: 3 } },
    ] }],
  ])), { problems: [], notes: [] });
});

test("scenario payloads and expectations do not drive their op members", () => {
  assert.deepEqual(driverInventory(driver("peer.call"), new Map([
    ["conformance/scenarios/peer/data.json", { steps: [{
      on: "a", op: "peer.call",
      args: { params: { op: "application.input" } },
      expect: { op: "application.output" },
    }] }],
  ])), { problems: [], notes: [] });
});

test("inventory diagnostics are ordered by scenario and op and deduplicate repeated steps", () => {
  const result = driverInventory(driver("peer.z", "peer.a"), new Map([
    ["conformance/scenarios/z.json", scenario("conn.send")],
    ["conformance/scenarios/a.json", scenario("peer.zed", "peer.absent", "peer.absent")],
  ]));
  assert.deepEqual(result.problems, [
    { page: "conformance/scenarios/a.json", reason: "drives peer.absent, which has no op row in conformance/DRIVER.md" },
    { page: "conformance/scenarios/a.json", reason: "drives peer.zed, which has no op row in conformance/DRIVER.md" },
    { page: "conformance/scenarios/z.json", reason: "drives conn.send, which has no op row in conformance/DRIVER.md" },
  ]);
  assert.deepEqual(result.notes, [
    { page: "conformance/DRIVER.md", reason: "documents peer.a, which no scenario drives" },
    { page: "conformance/DRIVER.md", reason: "documents peer.z, which no scenario drives" },
  ]);
});

test("only op table first cells document operations, not prose, arguments or observer events", () => {
  const markdown = [
    "A mention of `peer.prose` is not a row.",
    driver("peer.call"),
    "| `peer.emit`, `peer.await_event` | uses `peer.argument` | {} |",
    "",
    "| event | members |",
    "| --- | --- |",
    "| `request.started` | id |",
    "",
    "| op | arguments |",
    "This is not a table delimiter.",
    "| `peer.orphan` | none |",
  ].join("\n");
  assert.deepEqual([...driverOps(markdown)].sort(), ["peer.await_event", "peer.call", "peer.emit"]);
});

test("op tables in fenced examples do not document operations", () => {
  const markdown = [
    "````markdown", driver("peer.example"), "```", driver("peer.still_example"), "````",
    "~~~markdown", "<!-- a comment in an example opens no real comment", driver("peer.tilde_example"), "~~~",
    driver("peer.call"),
  ].join("\r\n");
  assert.deepEqual([...driverOps(markdown)], ["peer.call"]);
});

test("a prose mention cannot hide a scenario op missing its driver row", () => {
  const result = driverInventory("See `peer.forgotten` in a future table.\n", new Map([
    ["conformance/scenarios/peer/missing.json", scenario("peer.forgotten")],
  ]));
  assert.equal(result.problems.length, 1);
  assert.match(result.problems[0].reason, /peer\.forgotten/);
});

test("HTML comments cannot hide a missing driver row", () => {
  const markdown = [
    "<!--", driver("peer.hidden"), "-->",
    "<!-- " + driver("peer.inline").replaceAll("\n", " -->\n<!-- ") + " -->",
    "<!-- ``` -->",
    driver("peer.call"),
  ].join("\n");
  const result = driverInventory(markdown, new Map([
    ["conformance/scenarios/peer/missing.json", scenario("peer.hidden", "peer.inline", "peer.call")],
  ]));
  assert.equal(result.problems.length, 2);
  assert.match(result.problems[0].reason, /peer\.hidden/);
  assert.match(result.problems[1].reason, /peer\.inline/);
  assert.deepEqual(result.notes, []);
});

test("an op table needs a whole delimiter row with the header's column count", () => {
  for (const delimiter of ["| --- | prose | --- |", "| --- | --- |", "| --- | --- | --- | --- |"]) {
    const markdown = ["| op | arguments | answer |", delimiter, "| `peer.forgotten` | none | {} |"].join("\n");
    assert.deepEqual([...driverOps(markdown)], [], delimiter);
  }
});
