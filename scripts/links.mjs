// Holds every link in every tracked Markdown page to the tree, the way
// `nightseam check` holds generated output and matrix-table.mjs holds the
// README's table: a link is a claim that something is where it says, a
// reader takes it for the truth, and a page moved without its inbound links
// breaks each of them silently. So CI runs this on every pull request.
//
//	node scripts/links.mjs    # fail, naming each, if any link does not resolve
//
// What it holds: a relative link — `[text](path)`, `[text](path#anchor)`,
// `[text](#anchor)`, `[id]: path`, an image — resolves to a tracked file or a
// directory that holds one, and an anchor on a Markdown target names one of
// its headings by GitHub's slug. The repository's own absolute URLs,
// `https://github.com/Bitspark/nightseam/blob/main/<path>`, are held the
// same way, since that is the form a package README must use to render on
// npm and the one a move would otherwise break without a sound. Any other
// URL is somebody else's and is not asked. Links inside fenced or inline
// code are examples, not claims, and are skipped. The tracked set comes from
// git rather than the disk so that the answer is the same on a
// case-insensitive filesystem as on the one CI runs.
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join, posix } from "node:path";
import { pathToFileURL } from "node:url";
import { root } from "./packages.mjs";

/** The repository's own URLs, whose path after the ref is a path in the tree. */
export const ownURL = /^https:\/\/github\.com\/Bitspark\/nightseam\/(?:blob|tree)\/main(?:\/([^#]*))?(#.*)?$/;

/**
 * Every link a page makes, as `{line, target}`, outside fenced and inline
 * code: inline links and images, and reference definitions. Autolinks in
 * angle brackets are URLs and are not returned.
 */
export function links(markdown) {
  const found = [];
  let fence = null;
  markdown.split("\n").forEach((raw, index) => {
    const opening = raw.match(/^\s{0,3}(`{3,}|~{3,})/);
    if (fence) {
      if (opening && opening[1][0] === fence[0] && opening[1].length >= fence.length) fence = null;
      return;
    }
    if (opening) {
      fence = opening[1];
      return;
    }
    const line = raw.replace(/`[^`]*`/g, "");
    const definition = line.match(/^\s{0,3}\[[^\]]+\]:\s*(\S+)/);
    if (definition) {
      found.push({ line: index + 1, target: unwrap(definition[1]) });
      return;
    }
    for (const match of line.matchAll(/!?\[(?:[^\[\]]|\[[^\]]*\])*\]\(([^()\s]+(?:\([^()]*\))?[^()\s]*)(?:\s+"[^"]*")?\)/g)) {
      found.push({ line: index + 1, target: unwrap(match[1]) });
    }
  });
  return found;
}

function unwrap(target) {
  return target.startsWith("<") && target.endsWith(">") ? target.slice(1, -1) : target;
}

/** A heading's anchor as GitHub renders it; `taken` counts repeats so that the second `## Options` is `options-1`. */
export function slug(heading, taken = new Map()) {
  const text = heading
    .replace(/`([^`]*)`/g, "$1")
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    .trim()
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\p{M} _-]/gu, "")
    .replace(/ /g, "-");
  const seen = taken.get(text) ?? 0;
  taken.set(text, seen + 1);
  return seen === 0 ? text : `${text}-${seen}`;
}

/** The anchors a page answers to: one per ATX heading outside fenced code. */
export function anchors(markdown) {
  const taken = new Map();
  const found = new Set();
  let fence = null;
  for (const raw of markdown.split("\n")) {
    const opening = raw.match(/^\s{0,3}(`{3,}|~{3,})/);
    if (fence) {
      if (opening && opening[1][0] === fence[0] && opening[1].length >= fence.length) fence = null;
      continue;
    }
    if (opening) {
      fence = opening[1];
      continue;
    }
    const heading = raw.match(/^\s{0,3}#{1,6}\s+(.*?)\s*#*\s*$/);
    if (heading) found.add(slug(heading[1], taken));
  }
  return found;
}

/**
 * Every link of every page in `pages` (path → Markdown, paths POSIX and
 * relative to the root) held to `tracked`, the set of every tracked path.
 * Returns one `{page, line, target, reason}` per link that does not resolve.
 */
export function check(pages, tracked) {
  const directories = new Set();
  for (const path of tracked) {
    for (let at = path.lastIndexOf("/"); at > 0; at = path.lastIndexOf("/", at - 1)) directories.add(path.slice(0, at));
  }
  const exists = path => path === "" || tracked.has(path) || directories.has(path);
  const problems = [];
  for (const [page, markdown] of pages) {
    for (const { line, target } of links(markdown)) {
      let path, fragment;
      const own = target.match(ownURL);
      if (own) {
        path = own[1] ?? "";
        fragment = own[2] ? own[2].slice(1) : undefined;
      } else if (/^[a-z][a-z0-9+.-]*:/i.test(target) || target.startsWith("//")) {
        continue;
      } else {
        const hash = target.indexOf("#");
        const relative = hash < 0 ? target : target.slice(0, hash);
        fragment = hash < 0 ? undefined : target.slice(hash + 1);
        path = relative === "" ? page : posix.normalize(posix.join(posix.dirname(page), decodeURI(relative)));
        if (path === ".") path = "";
      }
      path = path.replace(/\/+$/, "");
      if (!exists(path)) {
        problems.push({ page, line, target, reason: `${path || "/"} is not in the tree` });
        continue;
      }
      if (fragment !== undefined && fragment !== "") {
        if (!path.endsWith(".md")) continue;
        const markdown = pages.get(path);
        if (markdown === undefined) {
          problems.push({ page, line, target, reason: `${path} is not a page this holds` });
          continue;
        }
        if (!anchors(markdown).has(fragment.toLowerCase())) {
          problems.push({ page, line, target, reason: `${path} has no heading "${fragment}"` });
        }
      }
    }
  }
  return problems;
}

// Run as a script; imported by the tests, which call the functions above.
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const tracked = new Set(
    execFileSync("git", ["ls-files", "-z"], { cwd: root, encoding: "utf8" }).split("\0").filter(Boolean),
  );
  // The tree's paths are POSIX whatever the platform; the disk is read by
  // the platform's own join, and that is the one place it is read.
  const pages = new Map();
  for (const path of tracked) {
    if (path.endsWith(".md")) pages.set(path, readFileSync(join(root, path), "utf8"));
  }
  const problems = check(pages, tracked);
  for (const { page, line, target, reason } of problems) console.error(`${page}:${line}: ${target} — ${reason}`);
  if (problems.length > 0) {
    console.error(`${problems.length} link${problems.length === 1 ? "" : "s"} do not resolve; a page that moved takes its inbound links with it.`);
    process.exit(1);
  }
  let count = 0;
  for (const markdown of pages.values()) count += links(markdown).length;
  console.log(`${count} links in ${pages.size} pages resolve`);
}
