// Holds what the documentation claims about its own shape, the way
// scripts/links.mjs holds every link and matrix-table.mjs holds the README's
// table: an index is a claim that a directory is covered, and a page added
// without its row is missed by every reader who navigates by the index rather
// than by `ls`. links.mjs proves a row points at a page that exists; nothing
// proved a page has a row, which is how the Unicode decision sat unindexed
// from #182 and proof-findings.md was never listed among the declaration
// pages. So CI runs this on every pull request.
//
//	node scripts/docs.mjs     # fail, naming each, on a page no index covers
//
// What it holds, in two claims:
//
//   - **Coverage.** Every Markdown page of a documented set is linked from
//     that set's index. The sets are below, each with the page that indexes
//     it — `docs/README.md` for the four state sets, and a README of its own
//     for the goals and the decisions, which are read as lists in themselves.
//     A page's own index does not have to be the only thing that links to it;
//     it has to be one of them, and exactly once: a page listed twice is what a
//     merge of two lanes each supplying a missing row leaves behind.
//
//   - **Form.** Every decision page carries the five parts its index promises
//     — the question, decided, why, serves, since — since that index states
//     them as what a page *has*, and a review that reads the record before
//     reopening a decision is reading for the *why* and the *serves*
//     specifically. The *why* is matched by its opening word alone, because a
//     page recording two verdicts says which it is arguing against, and
//     decisions/README.md allows that page.
//
// What it does not hold is prose: which sets exist, what an index row says
// about a page and in what order the rows stand stays editorial. A page
// missing from the directory it is indexed under is links.mjs's to refuse,
// and is left there.
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join, posix } from "node:path";
import { pathToFileURL } from "node:url";
import { root } from "./packages.mjs";
import { decodeMarkdown, links, ownURL } from "./links.mjs";

/** A documented set: the directory of pages, and the page that indexes it. */
export const sets = [
  { directory: "docs/goals", index: "docs/goals/README.md" },
  { directory: "docs/decisions", index: "docs/decisions/README.md" },
  { directory: "docs/wire", index: "docs/README.md" },
  { directory: "docs/runtime", index: "docs/README.md" },
  { directory: "docs/declaration", index: "docs/README.md" },
  { directory: "docs/languages", index: "docs/README.md" },
];

/** The five parts decisions/README.md says every decision page has. */
export const parts = ["The question", "Decided", "Why", "Serves", "Since"];

/**
 * Every tree path an index links to, resolved against the index's own
 * directory. The repository's own absolute URLs count: a package README
 * indexes by that form so as to render on npm, and an index that used it
 * would be covering its pages just the same.
 */
export function listing(markdown, index) {
  const directory = posix.dirname(index);
  const counted = new Map();
  for (const { target } of links(markdown)) {
    const own = target.match(ownURL);
    const path = (own ? (own[1] ?? "") : target).split("#")[0];
    if (!path || /^[a-z][a-z0-9+.-]*:/i.test(path)) continue;
    const resolved = own ? path : posix.normalize(posix.join(directory, path));
    counted.set(resolved, (counted.get(resolved) ?? 0) + 1);
  }
  return counted;
}

/** The pages an index links to, however often it links to each. */
export function covered(markdown, index) {
  return new Set(listing(markdown, index).keys());
}

/** The pages of a set, in tree order: its Markdown but for the index itself. */
export function pagesOf(directory, tracked) {
  const prefix = directory + "/";
  return [...tracked]
    .filter(path => path.startsWith(prefix) && path.endsWith(".md"))
    .filter(path => !path.slice(prefix.length).includes("/"))
    .filter(path => !path.endsWith("/README.md"))
    .sort();
}

/** A page of a set its index does not link to. */
export function uncovered(pages, tracked) {
  const problems = [];
  for (const { directory, index } of sets) {
    const markdown = pages.get(index);
    if (markdown === undefined) {
      problems.push({ page: index, reason: `indexes ${directory} and is not in the tree` });
      continue;
    }
    const linked = covered(markdown, index);
    for (const path of pagesOf(directory, tracked)) {
      if (!linked.has(path)) problems.push({ page: path, reason: `is under ${directory} and ${index} does not link to it` });
    }
  }
  return problems;
}

/**
 * A page its index lists more than once. No index in the tree does, and the
 * shape that would make one is two lanes each adding the row a page was
 * missing and a merge taking both: the page is covered twice and a reader
 * meets it twice.
 */
export function duplicated(pages, tracked) {
  const problems = [];
  for (const { directory, index } of sets) {
    const markdown = pages.get(index);
    if (markdown === undefined) continue;
    const counted = listing(markdown, index);
    for (const path of pagesOf(directory, tracked)) {
      const times = counted.get(path) ?? 0;
      if (times > 1) problems.push({ page: path, reason: `is listed ${times} times by ${index}` });
    }
  }
  return problems;
}

/** A decision page missing one of the five parts its index promises. */
export function formless(pages, tracked) {
  const problems = [];
  for (const path of pagesOf("docs/decisions", tracked)) {
    const markdown = pages.get(path);
    if (markdown === undefined) continue;
    const missing = parts.filter(part => !new RegExp(`\\*\\*${part}[^*]*\\.\\*\\*`).test(markdown));
    if (missing.length > 0) problems.push({ page: path, reason: `is a decision without its ${missing.join(", ")}` });
  }
  return problems;
}

// Run as a script; imported by the tests, which call the functions above.
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const tracked = new Set(
    execFileSync("git", ["ls-files", "-z", "docs"], { cwd: root, encoding: "utf8" }).split("\0").filter(Boolean),
  );
  const pages = new Map();
  for (const path of tracked) {
    if (!path.endsWith(".md")) continue;
    try {
      pages.set(path, decodeMarkdown(readFileSync(join(root, path)), path));
    } catch (error) {
      console.error(error.message);
      process.exit(1);
    }
  }
  const problems = [...uncovered(pages, tracked), ...duplicated(pages, tracked), ...formless(pages, tracked)];
  for (const { page, reason } of problems) console.error(`${page}: ${reason}`);
  if (problems.length > 0) {
    console.error(`${problems.length} page${problems.length === 1 ? "" : "s"} the documentation does not account for; an index is a claim that a directory is covered.`);
    process.exit(1);
  }
  const counted = sets.reduce((total, { directory }) => total + pagesOf(directory, tracked).length, 0);
  console.log(`${counted} pages in ${sets.length} sets are indexed, and every decision carries its five parts`);
}
