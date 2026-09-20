// What the documentation check reads as coverage and as the decision form, on
// pages no tree has — an index that lists all but one of a directory, a page
// reached only by an absolute URL of this repository, a decision missing its
// serves — because a check first exercised by the page that slips past it is a
// check nobody has run.
import assert from "node:assert/strict";
import { test } from "node:test";
import { covered, duplicated, formless, pagesOf, parts, sets, uncovered } from "./docs.mjs";

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
