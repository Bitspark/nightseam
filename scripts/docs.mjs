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
// What it holds:
//
//   - **Coverage.** Every Markdown page of a documented set is linked from
//     that set's index. The sets are below, each with the page that indexes
//     it — `docs/README.md` for the four state sets, and a README of its own
//     for the goals and the decisions, which are read as lists in themselves.
//     A page's own index does not have to be the only thing that links to it;
//     it has to be one of them, and exactly once: a page listed twice is what a
//     merge of two lanes each supplying a missing row leaves behind.
//
//   - **Built-in references.** Every built-in family's reference is listed by
//     the index beside it. These are not a set: a set's pages are the Markdown
//     directly under it, and a reference is a directory holding the page the
//     spec target renders, so `pagesOf` sees none of them. The generator's
//     golden test writes a page per `builtin.Names()`, which leaves the row the
//     only part a lane owes — and `live` went without one from the tier that
//     added it until this claim.
//
//   - **Form.** Every decision page carries the five parts its index promises
//     — the question, decided, why, serves, since — since that index states
//     them as what a page *has*, and a review that reads the record before
//     reopening a decision is reading for the *why* and the *serves*
//     specifically. The *why* is matched by its opening word alone, because a
//     page recording two verdicts says which it is arguing against, and
//     decisions/README.md allows that page.
//
//   - **Driver inventory.** Every op a scenario drives has a row in DRIVER.md,
//     so a testee written from that document can run the scenarios. A row no
//     scenario drives is only a note: it may precede the scenario that uses it.
//
//   - **Credit.** A possessive proper noun names one of the things this
//     documentation speaks of. A page crediting anything else tells a reader
//     about a repository they cannot open, which this repository decided not
//     to do: `b7cbe5d` cut the release scaffolding under "the repository
//     self-contained, no other repository named in it", and `b705fa3` swept
//     the neighbour surveys out "before this one is made public", since they
//     carried "the architecture of code that is not published, which a public
//     repository would hand to anyone who cloned it". Four credits stood in
//     pages that sweep did not reach and outlived it — a decision page giving
//     its two rejected shapes as two siblings', the admission table's prior
//     art, a tiers page deferring to a generator nobody can read. The list is
//     of what may be credited and never of what may not: a denylist of
//     unpublished names would be the leak itself, committed, grepped and kept
//     forever. A link to one of the organization's repositories credits it
//     too, and says more — a path into a design nobody outside can open —
//     so it is held to the same list.
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
  { directory: "docs/auth", index: "docs/README.md" },
];

/**
 * The built-in family references, which are not a set: a set's pages are the
 * Markdown directly under it, and these are a directory per family, each
 * holding the `README.md` the spec target renders for it.
 */
export const builtins = { directory: "docs/declaration/builtins", index: "docs/declaration/builtins/README.md" };

/** The five parts decisions/README.md says every decision page has. */
export const parts = ["The question", "Decided", "Why", "Serves", "Since"];

/**
 * What a page may credit: the languages, runtimes, registries and standards
 * this documentation speaks of, each one a reader can look up. It is derived
 * from the tree rather than imagined for it — these are the possessives the
 * pages already carry — so a name arriving here is a lane saying out loud
 * that a new thing is now spoken of, which is the point of the row.
 */
export const attributable = new Set([
  "Archon",
  "Bitwire",
  "CI",
  "ECMAScript",
  "GitHub",
  "Go",
  "JavaScript",
  "Nightseam",
  "Node",
  "PR",
  "Python",
  "RE2",
  "README",
  "TypeScript",
]);

/** The directories whose Markdown a target renders, where no hand writes. */
export const rendered = ["cmd/nightseam/testdata/", "examples/probe/api/"];

/**
 * The Markdown a hand wrote: every tracked page outside the generator's own
 * output. The credit claim reads all of it and not the documented sets alone,
 * since the pages a reader meets first — the README, COLLABORATION, a
 * package's own README on npm — are the ones no set covers.
 */
export function handwritten(paths) {
  return [...paths]
    .filter(path => path.endsWith(".md"))
    .filter(path => !rendered.some(prefix => path.startsWith(prefix)))
    .sort();
}

// These words followed by 's contract is, has or us; they credit no name.
const contractions = new Set(["He", "She", "It", "That", "Here", "There", "What", "Who", "Where", "When", "Why", "How", "Let"]);

/**
 * A page crediting a name outside `attributable`, once per name however often
 * the page uses it. A possessive is the attributive form — it says what
 * another party chose and that this repository listened — which is why the
 * claim holds it and not every capitalized word: a rule refusing those would
 * refuse the first word of most sentences.
 *
 * A single capital is not a name and is skipped: the suite calls its peers A,
 * B and C, and `A's scope` credits nobody, since the letter names nothing
 * outside the scenario that binds it.
 *
 * A link to `github.com/Bitspark/<repository>` credits the repository as a
 * possessive would, matched against the same names whatever its case, since
 * an address is written in lower case where prose capitalizes: a page
 * linking a repository the documentation does not speak of is refused once
 * per repository, however often it links it.
 */
export function unattributable(pages, paths) {
  const problems = [];
  const repositories = new Set([...attributable].map(name => name.toLowerCase()));
  for (const path of handwritten(paths)) {
    const markdown = pages.get(path);
    if (markdown === undefined) continue;
    const credited = new Set();
    for (const [, name] of markdown.matchAll(/\b([A-Z][A-Za-z0-9]+)['’]s\b/g)) {
      if (!attributable.has(name) && !contractions.has(name)) credited.add(name);
    }
    for (const name of [...credited].sort()) {
      problems.push({ page: path, reason: `credits ${name}, which is not a name this documentation speaks of` });
    }
    const linked = new Set();
    for (const [, repository] of markdown.matchAll(/github\.com\/Bitspark\/([A-Za-z0-9_-]+)/gi)) {
      if (!repositories.has(repository.toLowerCase())) linked.add(repository.toLowerCase());
    }
    for (const repository of [...linked].sort()) {
      problems.push({ page: path, reason: `links the repository ${repository}, which is not a name this documentation speaks of` });
    }
  }
  return problems;
}

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

/**
 * The references under a directory that holds one per family: the `README.md`
 * exactly one level below it. `pagesOf` cannot see them — it keeps a set's
 * direct Markdown children and drops every `README.md` — which is why the
 * built-in references needed a claim of their own rather than a row in `sets`.
 */
export function referencesOf(directory, tracked) {
  const prefix = directory + "/";
  return [...tracked]
    .filter(path => path.startsWith(prefix) && path.endsWith("/README.md"))
    .filter(path => path.slice(prefix.length).split("/").length === 2)
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

/**
 * A built-in family reference its index does not list. `TestBuiltinSpecificationsGolden`
 * renders one page per `builtin.Names()`, so a built-in added to the generator
 * arrives with a page already written and a row nobody owes — which is how
 * `live` stood in the tree, generated and held, beside an index naming `duplex`
 * and `tunnel` alone.
 */
export function unlisted(pages, tracked) {
  const markdown = pages.get(builtins.index);
  if (markdown === undefined) {
    return [{ page: builtins.index, reason: `indexes ${builtins.directory} and is not in the tree` }];
  }
  const linked = covered(markdown, builtins.index);
  return referencesOf(builtins.directory, tracked)
    .filter(path => !linked.has(path))
    .map(path => ({ page: path, reason: `is a built-in family reference and ${builtins.index} does not link to it` }));
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

const driverPath = "conformance/DRIVER.md";

/** Operations in the first column of op tables, outside fenced examples. */
export function driverOps(markdown) {
  const found = new Set();
  let fence = null;
  let comment = false;
  let header = 0;
  let table = false;
  for (const raw of markdown.split(/\r?\n/)) {
    if (fence) {
      const closing = raw.match(/^ {0,3}(`{3,}|~{3,})\s*$/);
      if (closing && closing[1][0] === fence[0] && closing[1].length >= fence.length) fence = null;
      continue;
    }
    let line = "";
    let rest = raw;
    while (rest) {
      const at = rest.indexOf(comment ? "-->" : "<!--");
      if (at < 0) {
        if (!comment) line += rest;
        break;
      }
      if (!comment) line += rest.slice(0, at);
      rest = rest.slice(at + (comment ? 3 : 4));
      comment = !comment;
    }
    const opening = line.match(/^ {0,3}(`{3,}|~{3,})/);
    if (opening) {
      fence = opening[1];
      header = 0;
      table = false;
      continue;
    }
    if (!/^ {0,3}\|[^|]*\|/.test(line)) {
      header = 0;
      table = false;
      continue;
    }
    const cells = line.trim().slice(1).replace(/\|$/, "").split("|").map(cell => cell.trim());
    const cell = cells[0];
    if (header) {
      table = cells.length === header && cells.every(value => /^:?-+:?$/.test(value));
      header = 0;
      continue;
    }
    if (cell === "op") {
      header = cells.length;
      table = false;
      continue;
    }
    if (!table) continue;
    for (const match of cell.matchAll(/`([a-z]+\.[a-z_]+)`/g)) found.add(match[1]);
  }
  return found;
}

/**
 * A scenario's steps drive ops; op members inside arguments or expected values
 * are application data. Missing rows refuse, while unused rows only note.
 */
export function driverInventory(markdown, scenarios) {
  const documented = driverOps(markdown);
  const driven = new Set();
  const problems = [];
  for (const page of [...scenarios.keys()].sort()) {
    const ops = new Set(scenarios.get(page).steps.map(step => step.op));
    for (const op of [...ops].sort()) {
      driven.add(op);
      if (!documented.has(op)) problems.push({ page, reason: `drives ${op}, which has no op row in ${driverPath}` });
    }
  }
  const notes = [...documented].sort()
    .filter(op => !driven.has(op))
    .map(op => ({ page: driverPath, reason: `documents ${op}, which no scenario drives` }));
  return { problems, notes };
}

// Run as a script; imported by the tests, which call the functions above.
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const tracked = new Set(
    execFileSync("git", ["ls-files", "-z", "docs", driverPath, "conformance/scenarios"], { cwd: root, encoding: "utf8" }).split("\0").filter(Boolean),
  );
  const credited = handwritten(
    execFileSync("git", ["ls-files", "-z", "*.md"], { cwd: root, encoding: "utf8" }).split("\0").filter(Boolean),
  );
  const pages = new Map();
  const scenarios = new Map();
  for (const path of new Set([...tracked, ...credited])) {
    try {
      if (path.endsWith(".md")) pages.set(path, decodeMarkdown(readFileSync(join(root, path)), path));
      else if (path.endsWith(".json") && path.startsWith("conformance/scenarios/")) {
        scenarios.set(path, JSON.parse(readFileSync(join(root, path), "utf8")));
      }
    } catch (error) {
      console.error(`${path}: ${error.message}`);
      process.exit(1);
    }
  }
  const inventory = driverInventory(pages.get(driverPath) ?? "", scenarios);
  const problems = [
    ...uncovered(pages, tracked),
    ...duplicated(pages, tracked),
    ...unlisted(pages, tracked),
    ...formless(pages, tracked),
    ...unattributable(pages, credited),
    ...inventory.problems,
  ];
  for (const { page, reason } of problems) console.error(`${page}: ${reason}`);
  for (const { page, reason } of inventory.notes) console.log(`note: ${page}: ${reason}`);
  if (problems.length > 0) {
    console.error(`${problems.length} documentation claim${problems.length === 1 ? "" : "s"} not satisfied.`);
    process.exit(1);
  }
  const counted = sets.reduce((total, { directory }) => total + pagesOf(directory, tracked).length, 0);
  const references = referencesOf(builtins.directory, tracked).length;
  console.log(
    `${counted} pages in ${sets.length} sets and ${references} built-in references are indexed, every decision carries its five parts, every scenario op has a driver row, and ${credited.length} pages credit only what this documentation speaks of`,
  );
}
