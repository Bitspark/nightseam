// Read-only planning audit; it is not a merge gate and never edits GitHub.
//
//   node scripts/issues.mjs check
//   node scripts/issues.mjs check --snapshot plan.json --json report.json
//   node scripts/issues.mjs check --input plan.json
//
// Snapshots have {version: 1, repository: "owner/repo", issues: [...]}. Each
// issue has number, title, state, type (name or null), milestone (title/state
// or null), labels (names), body, parent (number or null), blockedBy (numbers)
// and comments (body/url). Referenced closed issues are included for context,
// not held to today's lane form. --snapshot records this normalized input.
//
// Only a direct Waits on field supplies prose prerequisites. Lists start with
// #N (optionally bulleted); explanations may follow. "Nothing"/"None" names
// no prerequisites. Combined design fields and other references are not edges.
// A Verdict section is reported as recorded text, never interpreted as approval
// or implementation completion. Free-form recommendations are not verdicts.

import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const run = promisify(execFile);
const fields = ["what", "what happens, and what should happen instead", "surface", "held by", "provenance", "waits on", "touches", "parity", "verdict", "waits on / unblocks", "dependency notes"];
const meaningful = text => text != null && !/^(?:\s*|_No response_|[-—]|pending\.?|no verdict yet\.?|not decided\.?)$/i.test(text.trim());
const number = value => Number.isSafeInteger(value) && value > 0;

/** Read one field, in either issue-form or handwritten shape, outside fences. */
export function field(body, wanted) {
  let fence = null;
  const text = body.split(/\r?\n/).map(line => {
    const marker = line.match(/^\s*(`{3,}|~{3,})/);
    if (marker && (!fence || marker[1][0] === fence[0] && marker[1].length >= fence.length)) {
      fence = fence ? null : marker[1];
      return "";
    }
    return fence ? "" : line;
  }).join("\n");
  // Mask inline code for recognition while retaining the original field value
  // (Touches legitimately contains backticks). Keep offsets and newlines.
  const scan = text.replace(/(?<!`)(`+)(?!`)[\s\S]*?(?<!`)\1(?!`)/g, code => code.replace(/[^\n]/g, "_"));
  const boundaries = [];
  const pattern = /^#{1,6}[ \t]+([^\n]+)|\*\*([^*\n]+)\*\*:?[ \t]*/gm;
  for (const match of scan.matchAll(pattern)) {
    const label = (match[1] ?? match[2]).trim().replace(/[:.]$/, "").toLowerCase();
    const lineStart = scan.lastIndexOf("\n", match.index - 1) + 1;
    const prefix = scan.slice(lineStart, match.index);
    const prior = boundaries.at(-1);
    // A bold label starts a field line, or follows a completed field on that
    // line. An emphasized word buried in prose cannot manufacture metadata.
    const fieldPosition = /^\s*(?:[-*+]\s+)?$/.test(prefix) ||
      prior?.start >= lineStart && /[.;!?]\s+$/.test(prefix);
    if (match[1] || fields.includes(label) && fieldPosition) boundaries.push({ label, start: match.index, end: match.index + match[0].length });
  }
  const at = boundaries.findIndex(entry => entry.label === wanted.toLowerCase());
  return at < 0 ? null : text.slice(boundaries[at].end, boundaries[at + 1]?.start).trim();
}

export function prerequisites(body) {
  const text = field(body, "waits on");
  if (text == null) return { present: false, numbers: [], ambiguous: false };
  if (/^(?:nothing|none)(?:[.!](?:\s|$)|\s*$)/i.test(text)) return { present: true, numbers: [], ambiguous: false };
  const found = [];
  for (const line of text.split("\n")) {
    let rest = line.replace(/^\s*(?:[-*+]\s+|\d+\.\s+)?/, "");
    const first = rest.match(/^#(\d+)\b/);
    if (!first) continue;
    found.push(Number(first[1]));
    rest = rest.slice(first[0].length);
    for (;;) {
      const next = rest.match(/^\s*(?:,\s*(?:and\s+)?|;\s*|and\s+|&\s+)#(\d+)\b/i);
      if (!next) break;
      found.push(Number(next[1]));
      rest = rest.slice(next[0].length);
    }
  }
  return { present: true, numbers: [...new Set(found)], ambiguous: !found.length };
}

function validate(snapshot) {
  if (snapshot?.version !== 1 || !/^[\w.-]+\/[\w.-]+$/.test(snapshot.repository ?? "") || !Array.isArray(snapshot.issues)) throw new Error("invalid issue snapshot header");
  const seen = new Set();
  for (const issue of snapshot.issues) {
    if (!number(issue.number) || seen.has(issue.number)) throw new Error(`invalid or duplicate issue number ${issue.number}`);
    seen.add(issue.number);
    if (!["open", "closed"].includes(issue.state)) throw new Error(`#${issue.number}: invalid state`);
    if (typeof issue.body !== "string" || typeof issue.title !== "string" || !(issue.type === null || typeof issue.type === "string")) throw new Error(`#${issue.number}: incomplete issue snapshot`);
    if (!(issue.milestone === null || ["open", "closed"].includes(issue.milestone?.state) && typeof issue.milestone.title === "string")) throw new Error(`#${issue.number}: incomplete milestone`);
    if (!(issue.parent === null || number(issue.parent))) throw new Error(`#${issue.number}: missing or invalid parent`);
    if (!Array.isArray(issue.blockedBy) || !issue.blockedBy.every(number)) throw new Error(`#${issue.number}: missing or invalid blockedBy`);
    if (!Array.isArray(issue.labels) || !issue.labels.every(label => typeof label === "string")) throw new Error(`#${issue.number}: invalid labels`);
    if (!Array.isArray(issue.comments) || !issue.comments.every(comment => typeof comment.body === "string" && typeof comment.url === "string")) throw new Error(`#${issue.number}: invalid comments`);
  }
}

/** Audit structure, retaining unresolved decisions and satisfied dependencies as status. */
export function audit(snapshot) {
  validate(snapshot);
  const all = new Map(snapshot.issues.map(issue => [issue.number, issue]));
  const active = snapshot.issues.filter(issue => issue.state === "open" && (!issue.milestone || issue.milestone.state === "open"));
  const report = { repository: snapshot.repository, active: active.length, findings: [], dependencies: [], designs: [] };
  const url = n => `https://github.com/${snapshot.repository}/issues/${n}`;
  const add = (issue, code, message) => report.findings.push({ number: issue.number, url: url(issue.number), code, message });
  for (const issue of active) {
    if (!issue.milestone) add(issue, "missing-milestone", "No milestone; place the issue on the board.");
    if (!issue.type) add(issue, "missing-type", "No issue type; choose Epic, Task or Bug.");
    else if (!["Epic", "Task", "Bug"].includes(issue.type)) add(issue, "unknown-type", `Unrecognized issue type ${issue.type}.`);
    if (issue.parent === null) {
      if (issue.type !== "Epic") add(issue, "missing-parent", "A lane needs a native Epic parent.");
    } else {
      const parent = all.get(issue.parent);
      if (!parent) add(issue, "missing-reference", `Parent #${issue.parent} is absent from the snapshot.`);
      else if (parent.type !== "Epic") add(issue, "parent-not-epic", `Parent #${parent.number} is not an Epic.`);
    }
    const design = issue.labels.includes("design");
    if (!design && ["Task", "Bug"].includes(issue.type)) {
      for (const name of ["what", "surface", "held by", "provenance", "waits on", "touches"]) {
        const content = field(issue.body, name) ?? (name === "what" ? field(issue.body, "what happens, and what should happen instead") : null);
        if (!meaningful(content)) add(issue, "missing-field", `Missing ${name} lane field.`);
      }
    }
    const waits = prerequisites(issue.body);
    if (waits.ambiguous) add(issue, "ambiguous-waits-on", "Waits on is not an explicit #N list or Nothing; review it without inferring edges from context.");
    if (waits.present && !waits.ambiguous) {
      for (const n of waits.numbers) if (!issue.blockedBy.includes(n)) add(issue, "missing-dependency", `Waits on #${n}, but the native blocked-by link is missing (closed prerequisites still need their link).`);
      for (const n of issue.blockedBy) if (!waits.numbers.includes(n)) add(issue, "unlisted-dependency", `Native blocker #${n} is absent from the explicit Waits on list.`);
    }
    for (const n of issue.blockedBy) {
      const dependency = all.get(n);
      if (!dependency) add(issue, "missing-reference", `Prerequisite #${n} is absent from the snapshot.`);
      else report.dependencies.push({ number: issue.number, prerequisite: n, state: dependency.state });
    }
  }
  // Closed decisions remain visible while active lanes depend on them. Their
  // missing historical lane fields do not become today's structural failures.
  for (const issue of snapshot.issues.filter(issue => issue.labels.includes("design"))) {
    const downstream = snapshot.issues.filter(candidate => candidate.blockedBy.includes(issue.number)).map(candidate => ({ number: candidate.number, state: candidate.state }));
    if (!active.includes(issue) && !active.some(candidate => candidate.blockedBy.includes(issue.number))) continue;
    const sources = [{ body: issue.body, url: url(issue.number) }, ...issue.comments];
    const recorded = sources.filter(source => meaningful(field(source.body, "verdict"))).at(-1);
    report.designs.push({ number: issue.number, state: issue.state, verdict: recorded ? "recorded" : "not-recorded", ...(recorded ? { source: recorded.url } : {}), downstream });
  }
  // Follow only graphs reachable from today's active plan; old unrelated cycles
  // and already-satisfied closed prerequisites are not current blockers.
  for (const [code, edges] of [
    ["parent-cycle", issue => issue.parent === null ? [] : [issue.parent]],
    ["dependency-cycle", issue => issue.state === "open" ? issue.blockedBy.filter(n => all.get(n)?.state === "open") : []],
  ]) {
    const visiting = [], done = new Set(), emitted = new Set();
    const visit = n => {
      const at = visiting.indexOf(n);
      if (at >= 0) {
        const cycle = visiting.slice(at), key = [...cycle].sort((a, b) => a - b).join(",");
        if (!emitted.has(key)) { emitted.add(key); add(all.get(n), code, `Cycle: ${[...cycle, n].map(v => `#${v}`).join(" → ")}.`); }
        return;
      }
      if (done.has(n) || !all.has(n)) return;
      visiting.push(n);
      for (const next of edges(all.get(n))) visit(next);
      visiting.pop(); done.add(n);
    };
    for (const issue of active) visit(issue.number);
  }
  report.findings.sort((a, b) => a.number - b.number || a.code.localeCompare(b.code));
  report.dependencies.sort((a, b) => a.number - b.number || a.prerequisite - b.prerequisite);
  report.designs.sort((a, b) => a.number - b.number);
  return report;
}

async function github(path, { list = false, absent = false } = {}) {
  try {
    const { stdout } = await run("gh", ["api", "--method", "GET", path, ...(list ? ["--paginate", "--slurp"] : [])], { encoding: "utf8", maxBuffer: 32 * 1024 * 1024 });
    const value = JSON.parse(stdout);
    return list ? value.flat() : value;
  } catch (error) {
    if (absent && /\(HTTP 404\)/.test(error.stderr ?? "")) return null;
    throw new Error(`GitHub read failed for ${path}: ${error.stderr || error.message}`);
  }
}

/** Collect parents and native blockers, including closed satisfied prerequisites. */
export async function readLive(repository, api = github) {
  if (!/^[\w.-]+\/[\w.-]+$/.test(repository)) throw new Error("invalid repository; expected owner/repo");
  const base = `repos/${repository}/issues`;
  const initial = await api(`${base}?state=open&per_page=100`, { list: true });
  const raw = new Map(initial.filter(issue => !issue.pull_request).map(issue => [issue.number, issue]));
  const loaded = new Set();
  const normalized = new Map();
  const normalize = issue => ({ number: issue.number, title: issue.title, state: issue.state,
    type: issue.type?.name ?? null, milestone: issue.milestone ? { title: issue.milestone.title, state: issue.milestone.state } : null,
    labels: (issue.labels ?? []).map(label => typeof label === "string" ? label : label.name), body: issue.body ?? "",
    parent: null, blockedBy: [], comments: [] });
  for (;;) {
    const batch = [...raw.values()].filter(issue => !loaded.has(issue.number)).slice(0, 4);
    if (!batch.length) break;
    await Promise.all(batch.map(async issue => {
      loaded.add(issue.number);
      const entry = normalize(issue);
      normalized.set(issue.number, entry);
      if (entry.labels.includes("design")) {
        const comments = await api(`${base}/${issue.number}/comments?per_page=100`, { list: true });
        entry.comments = comments.map(comment => ({ body: comment.body ?? "", url: comment.html_url }));
      }
      if (issue.state === "closed" || issue.milestone?.state === "closed") return;
      const [parent, blockers] = await Promise.all([
        api(`${base}/${issue.number}/parent`, { absent: true }),
        api(`${base}/${issue.number}/dependencies/blocked_by?per_page=100`, { list: true }),
      ]);
      entry.parent = parent?.number ?? null;
      entry.blockedBy = blockers.map(blocker => blocker.number);
      for (const related of [parent, ...blockers]) if (related && !raw.has(related.number)) raw.set(related.number, related);
      // A missing native edge still needs its referenced issue's state in the
      // snapshot, so the report can be reproduced without another API read.
      for (const n of prerequisites(entry.body).numbers) if (!raw.has(n)) {
        const referenced = await api(`${base}/${n}`, { absent: true });
        if (referenced && !referenced.pull_request) raw.set(n, referenced);
      }
    }));
  }
  return { version: 1, repository, issues: [...normalized.values()].sort((a, b) => a.number - b.number) };
}

function markdown(report) {
  const link = n => `[#${n}](https://github.com/${report.repository}/issues/${n})`;
  const lines = [`# Issue plan audit`, "", `${report.active} open issues in open milestones (including unplaced issues); ${report.findings.length} structural findings.`, "", "This is a read-only structural report, not release approval or a verdict on design prose.", ""];
  if (report.findings.length) for (const finding of report.findings) lines.push(`- ${link(finding.number)} **${finding.code}**: ${finding.message}`);
  else lines.push("No structural inconsistencies found.");
  if (report.designs.length) {
    lines.push("", "## Design status", "", "Only explicit Verdict sections in the issue or comments are recognized. Recorded text needs human review; recommendations elsewhere are not classified as verdicts.", "");
    for (const design of report.designs) lines.push(`- ${link(design.number)} (${design.state}): ${design.verdict === "recorded" ? `[verdict text recorded](${design.source})` : "no structured Verdict section; inspect the discussion without inferring a decision"}. Downstream work: ${design.downstream.length ? design.downstream.map(i => `${link(i.number)} (${i.state})`).join(", ") : "none linked in this snapshot"}.`);
  }
  if (report.dependencies.length) {
    lines.push("", "## Prerequisites", "");
    for (const edge of report.dependencies) lines.push(`- ${link(edge.number)} waits on ${link(edge.prerequisite)}: ${edge.state === "closed" ? "satisfied (closed)" : "open"}.`);
  }
  return lines.join("\n") + "\n";
}

async function main(args) {
  if (args.shift() !== "check") throw new Error("usage: node scripts/issues.mjs check [--input snapshot.json] [--snapshot snapshot.json] [--json report.json]");
  const options = {};
  while (args.length) {
    const flag = args.shift();
    if (!["--input", "--snapshot", "--json"].includes(flag) || options[flag] || !args.length || args[0].startsWith("--")) throw new Error(`invalid option ${flag}`);
    options[flag] = args.shift();
  }
  const snapshot = options["--input"] ? JSON.parse(readFileSync(options["--input"], "utf8")) : await readLive(process.env.GITHUB_REPOSITORY ?? "Bitspark/nightseam");
  const report = audit(snapshot);
  if (options["--snapshot"]) writeFileSync(options["--snapshot"], JSON.stringify(snapshot, null, 2) + "\n");
  if (options["--json"]) writeFileSync(options["--json"], JSON.stringify(report, null, 2) + "\n");
  process.stdout.write(markdown(report));
  process.exitCode = report.findings.length ? 1 : 0;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main(process.argv.slice(2)).catch(error => { console.error(error.message); process.exitCode = 2; });
}
