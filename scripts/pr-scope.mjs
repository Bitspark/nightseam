// What a pull request must say about itself, checked once on open and on
// every push, so that the rules of COLLABORATION.md's "How a change lands"
// are held by a check rather than remembered:
//
//   - the PR closes exactly one issue, named `Closes #N` in its body;
//   - that issue has a milestone (an issue with none is not on the board,
//     which is how three audit findings fell through placement on
//     2026-09-19);
//   - the PR's title is one prose sentence prefixed by the area it changes,
//     which is what the squash commit will say;
//   - nothing in the title or body is a trailer or an attribution line.
//
// One thing is reported and never refused: the directories the PR changes
// against the issue's **Touches**, in either shape the tree holds — the
// form's `### Touches` heading or a hand-written bold `**Touches:**`, read by
// scripts/touches.mjs. A lane out of its box is visible in a comment, not
// blocked — the issue may have guessed the files wrong, and a lane that knows
// better should not have to edit the issue to land. An issue naming no
// Touches gets a note saying so, because a check that holds nothing and
// reports nothing reads exactly like one that passed.
//
//   node scripts/pr-scope.mjs <number>       check that PR, exit 1 on a refusal
//
// Runs in CI with the default token: reading a PR and an issue and writing
// a comment need `pull-requests: write` and `issues: read`, which ci.yml
// grants to this job alone.

import { execFileSync } from "node:child_process";
import { scopeNote } from "./touches.mjs";

const repo = process.env.GITHUB_REPOSITORY ?? "Bitspark/nightseam";
const number = process.argv[2];
if (!number) { console.error("usage: node scripts/pr-scope.mjs <pr-number>"); process.exit(2); }

const gh = (args, input) => execFileSync("gh", args, { encoding: "utf8", input, stdio: ["pipe", "pipe", "inherit"] });
const api = path => JSON.parse(gh(["api", path]));

const pr = api(`repos/${repo}/pulls/${number}`);
const title = pr.title ?? "";
const body = pr.body ?? "";
const refusals = [];

// The one issue this PR closes.
const closes = [...body.matchAll(/\b(?:closes|closed|close|fixes|fixed|fix|resolves|resolved|resolve)\s+#(\d+)/gi)].map(m => Number(m[1]));
const unique = [...new Set(closes)];
if (unique.length !== 1) {
  refusals.push(`the body names ${unique.length === 0 ? "no" : unique.length} issue${unique.length === 1 ? "" : "s"} to close (${unique.map(n => "#" + n).join(", ") || "none"}); one lane is one issue — write \`Closes #N\` once`);
}
let issue = null;
if (unique.length === 1) {
  issue = api(`repos/${repo}/issues/${unique[0]}`);
  if (issue.pull_request) refusals.push(`#${unique[0]} is a pull request, not an issue`);
  else if (!issue.milestone) refusals.push(`#${unique[0]} has no milestone; place it (the board is the milestones) before landing work on it`);
}

// The title is the squash commit's first line.
if (!/^[a-z][a-z0-9/._-]*: \S/.test(title)) {
  refusals.push(`the title is not "<area>: <one sentence>" (got ${JSON.stringify(title)}); it becomes the commit`);
}
if (/co-authored-by|generated with|🤖/i.test(title + "\n" + body)) {
  refusals.push("the title or body carries a trailer or an attribution line; commits here carry none");
}

// Scope, reported not refused: changed top-level areas against the issue's Touches.
let note = "";
if (issue && !issue.pull_request) {
  // One name per line rather than one JSON array: --paginate concatenates a
  // document per page, so a pull request past the first page of files is not
  // parseable as one array.
  const files = gh(["api", `repos/${repo}/pulls/${number}/files`, "--paginate", "--jq", ".[].filename"]).split("\n").map(line => line.trim()).filter(Boolean);
  const areas = [...new Set(files.map(f => f.split("/").slice(0, 2).join("/")))].sort();
  note = scopeNote(issue, areas);
}

if (refusals.length) {
  console.error(`PR #${number} is refused by scripts/pr-scope.mjs:\n  - ${refusals.join("\n  - ")}`);
  process.exit(1);
}
console.log(`PR #${number}: closes #${unique[0]} (milestone ${issue.milestone.title}); title ok; no trailer`);
if (note) {
  // Comment once: skip if the same note is already there.
  const comments = gh(["api", `repos/${repo}/issues/${number}/comments`, "--paginate", "--jq", ".[].body"]).split("\n");
  if (!comments.some(c => c.startsWith("Scope note"))) {
    gh(["api", "--method", "POST", `repos/${repo}/issues/${number}/comments`, "-f", `body=${note}`]);
    console.log("scope note posted");
  } else console.log("scope note already posted");
}
