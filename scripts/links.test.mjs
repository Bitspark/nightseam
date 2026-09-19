// What the link check reads as a link and what it holds one to, on pages no
// tree has — a page that moved, an anchor that is not a heading, the
// repository's own URL — because a check first exercised by the move that
// breaks a link is a check nobody has run.
import assert from "node:assert/strict";
import { test } from "node:test";
import { anchors, check, decodeMarkdown, links, slug } from "./links.mjs";

test("Markdown must be UTF-8 before links and headings are read", () => {
  const page = "# A page — with an ellipsis … and a character outside the BMP 🧵\n";
  assert.equal(decodeMarkdown(Buffer.from(page, "utf8"), "page.md"), page);
  for (const bytes of [Buffer.from([0x85]), Buffer.from([0xe2, 0x80]), Buffer.from([0xed, 0xa0, 0x80])]) {
    assert.throws(() => decodeMarkdown(bytes, "bad.md"), /bad\.md: Markdown must be valid UTF-8/);
  }
});

test("every form of link is read, and code is not", () => {
  const page = [
    "See [the profile](wire/profile.md) and [its envelope](wire/profile.md#the-envelope \"title\").",
    "An image ![alt](../art/seam.png) and a bare [anchor](#order).",
    "[ref]: ../README.md",
    "Not a link: `[code](in/backticks.md)` and this:",
    "```",
    "[fenced](in/a/block.md)",
    "```",
    "~~~md",
    "[tilde](fenced.md)",
    "~~~",
    "Two on one line: [a](a.md) and [b](b.md), and <https://example.test/> is an autolink.",
  ].join("\n");
  assert.deepEqual(links(page), [
    { line: 1, target: "wire/profile.md" },
    { line: 1, target: "wire/profile.md#the-envelope" },
    { line: 2, target: "../art/seam.png" },
    { line: 2, target: "#order" },
    { line: 3, target: "../README.md" },
    { line: 11, target: "a.md" },
    { line: 11, target: "b.md" },
  ]);
});

test("a heading's slug is GitHub's", () => {
  assert.equal(slug("The profile: `nightseam.duplex/1`"), "the-profile-nightseamduplex1");
  assert.equal(slug("go.json and typescript.json"), "gojson-and-typescriptjson");
  assert.equal(slug("Seam — `conn.*`"), "seam--conn");
  assert.equal(slug("What a [consumer](x.md) builds on it"), "what-a-consumer-builds-on-it");
  const taken = new Map();
  assert.equal(slug("Options", taken), "options");
  assert.equal(slug("Options", taken), "options-1");
  assert.equal(slug("Options", taken), "options-2");
});

test("anchors are the headings outside fenced code", () => {
  const page = ["# Title", "", "## The envelope", "```", "## not a heading", "```", "### Ids and correlation ##", "Text # not a heading"].join("\n");
  assert.deepEqual([...anchors(page)], ["title", "the-envelope", "ids-and-correlation"]);
});

test("a link resolves to a tracked file, a directory that holds one, or a heading", () => {
  const tracked = new Set(["README.md", "docs/README.md", "docs/wire/profile.md", "runtime/ts/README.md", "runtime/ts/src/peer.ts"]);
  const pages = new Map([
    ["README.md", "[docs](docs/) [map](docs/README.md) [wire](docs/wire) [src](runtime/ts/src/peer.ts) [self](#title)\n# Title"],
    ["docs/README.md", "[up](../README.md) [profile](wire/profile.md#the-envelope) [gone](language.md) [no such heading](wire/profile.md#ids)"],
    ["docs/wire/profile.md", "# The profile\n## The envelope\n[root](../../README.md) [outside](../../../elsewhere.md) [dir](../../runtime/ts/)"],
    ["runtime/ts/README.md", "[own](https://github.com/Bitspark/nightseam/blob/main/docs/wire/profile.md#the-envelope) [moved](https://github.com/Bitspark/nightseam/blob/main/docs/profile.md) [tree](https://github.com/Bitspark/nightseam/tree/main/docs) [home](https://github.com/Bitspark/nightseam#readme) [other](https://example.test/docs/profile.md) [mail](mailto:x@y.z)"],
  ]);
  assert.deepEqual(check(pages, tracked), [
    { page: "docs/README.md", line: 1, target: "language.md", reason: "docs/language.md is not in the tree" },
    { page: "docs/README.md", line: 1, target: "wire/profile.md#ids", reason: 'docs/wire/profile.md has no heading "ids"' },
    { page: "docs/wire/profile.md", line: 3, target: "../../../elsewhere.md", reason: "../elsewhere.md is not in the tree" },
    { page: "runtime/ts/README.md", line: 1, target: "https://github.com/Bitspark/nightseam/blob/main/docs/profile.md", reason: "docs/profile.md is not in the tree" },
  ]);
});

test("an anchor on a page the check does not hold is a problem, on a file that is not a page it is not", () => {
  const tracked = new Set(["a.md", "b.md", "c.txt"]);
  const pages = new Map([["a.md", "[b](b.md#x) [c](c.txt#x)"]]);
  assert.deepEqual(check(pages, tracked), [{ page: "a.md", line: 1, target: "b.md#x", reason: "b.md is not a page this holds" }]);
});
