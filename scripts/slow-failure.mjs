// A workflow run that failed after its first five minutes leaves an issue
// asking how it could have failed sooner: the run, the job and step that
// failed, the minute it failed at, the last lines of that step, and the one
// question — what check would have refused this in under five minutes? A
// run that fails in its first five minutes files nothing: it already did
// what the issue would ask for. A repeat of the same failure — same
// workflow, same job, same step — comments on the open issue instead of
// filing another. COLLABORATION.md, *A lesson is a check, never a paragraph*.
//
//	node scripts/slow-failure.mjs <run-id>            # from the workflow, with GH_TOKEN and GH_REPO
//	node scripts/slow-failure.mjs --run run.json --jobs jobs.json [--excerpt log.txt] [--existing issues.json] --dry-run
//
// The decision is a function of the run's and its jobs' JSON, so that a
// test holds it on runs no workflow has produced; the script runs only when
// it is the program.
import { readFileSync, realpathSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

/** How long a run may take to fail before the failure is one to look into. */
export const budgetMs = 5 * 60_000;
export const label = "slow-failure";

/** The earliest failing step of the run, and how long the run had been going when it ended. */
export function firstFailure(run, jobs) {
  const started = Date.parse(run.run_started_at ?? run.created_at);
  if (!Number.isFinite(started)) return undefined;
  const failures = [];
  for (const job of jobs) {
    if (job.conclusion !== "failure") continue;
    const step = (job.steps ?? []).find(s => s.conclusion === "failure");
    const at = Date.parse(step?.completed_at ?? job.completed_at ?? "");
    if (!Number.isFinite(at)) continue;
    failures.push({ job: job.name, jobId: job.id, step: step?.name ?? "(the job, at no step)", completedAt: new Date(at).toISOString(), elapsedMs: at - started });
  }
  failures.sort((a, b) => a.elapsedMs - b.elapsedMs);
  return failures[0];
}

export const minute = ms => Math.ceil(ms / 60_000);

/** What names a repeat of this failure, kept in the issue's body where a search finds it. */
export function marker(run, first) {
  return `<!-- slow-failure: ${run.name} / ${first.job} / ${first.step} -->`;
}

export function title(run, first) {
  return `flows: ${run.name} failed at minute ${minute(first.elapsedMs)} in ${first.job} › ${first.step}; how could it fail in under five?`;
}

export function body(run, first, excerpt, mark) {
  const where = `${run.name} run ${run.html_url} (\`${run.event}\` on \`${run.head_branch}\`, ${String(run.head_sha ?? "").slice(0, 8)})`;
  const lines = [
    "## What",
    "",
    `${where} failed at minute ${minute(first.elapsedMs)}: job \`${first.job}\`, step \`${first.step}\`, which ended at ${first.completedAt} after the run started at ${run.run_started_at ?? run.created_at}. Find the check that would have refused this in under five minutes — a script test, a preflight step before the toolchains, a syntax check, a probe of the registry, a deadline — and land it as the fix; then close this. If the failure was a transient of the check itself — a download, a runner — say so and close it with the retry or the deadline that answers it. Waiting twenty minutes for a run that was going to fail is what this issue exists to stop.`,
    "",
    "Last lines of the failing step:",
    "",
    "```",
    (excerpt || "(no log excerpt)").trim().slice(-4000),
    "```",
    "",
    "## Surface",
    "",
    "None until the check is named.",
    "",
    "## Held by",
    "",
    "A check that fails the same defect in under five minutes, on every pull request or in the workflow's first job, and a note here of which one.",
    "",
    "## Provenance",
    "",
    `Filed by \`.github/workflows/slow-failure.yml\` for run ${run.id}; COLLABORATION.md, *A lesson is a check, never a paragraph*; #619.`,
    "",
    "## Waits on",
    "",
    "Nothing.",
    "",
    "## Touches",
    "",
    "To be named by the lane that answers it.",
    "",
    mark,
    "",
  ];
  return lines.join("\n");
}

export function comment(run, first, excerpt) {
  return [
    `Failed again at minute ${minute(first.elapsedMs)} in \`${first.job}\` › \`${first.step}\`: ${run.html_url} (\`${run.event}\` on \`${run.head_branch}\`).`,
    "",
    "```",
    (excerpt || "(no log excerpt)").trim().slice(-2000),
    "```",
    "",
  ].join("\n");
}

/**
 * What to do about a completed run: nothing, file an issue, or comment on
 * the open one that names the same failure. `existing` is the open issues
 * carrying the label; a body holding this failure's marker is the one.
 */
export function decide({ run, jobs, existing = [], excerpt = "" }) {
  if (run.conclusion !== "failure") return { action: "none", reason: `the run's conclusion is ${run.conclusion ?? "none"}` };
  const first = firstFailure(run, jobs);
  if (!first) return { action: "none", reason: "no job of the run failed" };
  if (first.elapsedMs <= budgetMs) {
    return { action: "none", reason: `it failed at minute ${minute(first.elapsedMs)}, within the five-minute budget`, first };
  }
  const mark = marker(run, first);
  const open = existing.find(issue => (issue.body ?? "").includes(mark));
  if (open) return { action: "comment", issue: open.number, first, marker: mark, comment: comment(run, first, excerpt) };
  return { action: "create", first, marker: mark, title: title(run, first), body: body(run, first, excerpt, mark) };
}

const gh = (args, options = {}) => execFileSync("gh", args, { encoding: "utf8", stdio: ["ignore", "pipe", "inherit"], ...options });
const api = path => JSON.parse(gh(["api", "--paginate", path]));

/** The last lines of the failing job's log, without their timestamps. */
function excerptOf(repo, first) {
  try {
    const log = gh(["api", `repos/${repo}/actions/jobs/${first.jobId}/logs`], { maxBuffer: 64 * 1024 * 1024 });
    return log
      .split(/\r?\n/)
      .map(line => line.replace(/^\S+T\S+Z /, ""))
      .filter(line => line.trim() !== "")
      .slice(-40)
      .join("\n");
  } catch {
    return "";
  }
}

/** The open milestone with the lowest version in its title, which is the next release. */
function nextMilestone(repo) {
  try {
    const milestones = api(`repos/${repo}/milestones?state=open&per_page=100`);
    const versioned = milestones
      .map(m => ({ title: m.title, parts: (m.title.match(/^(\d+)\.(\d+)\.(\d+)$/) ?? []).slice(1).map(Number) }))
      .filter(m => m.parts.length === 3)
      .sort((a, b) => a.parts[0] - b.parts[0] || a.parts[1] - b.parts[1] || a.parts[2] - b.parts[2]);
    return versioned[0]?.title;
  } catch {
    return undefined;
  }
}

export async function main(argv = process.argv.slice(2)) {
  const flag = name => {
    const at = argv.indexOf(name);
    return at < 0 ? undefined : argv[at + 1];
  };
  const dryRun = argv.includes("--dry-run");
  const repo = process.env.GH_REPO;
  const id = argv.find(a => /^\d+$/.test(a));
  let run, jobs, excerpt, existing;
  if (flag("--run")) {
    run = JSON.parse(readFileSync(flag("--run"), "utf8"));
    jobs = JSON.parse(readFileSync(flag("--jobs"), "utf8"));
    jobs = Array.isArray(jobs) ? jobs : jobs.jobs;
    excerpt = flag("--excerpt") ? readFileSync(flag("--excerpt"), "utf8") : "";
    existing = flag("--existing") ? JSON.parse(readFileSync(flag("--existing"), "utf8")) : [];
  } else if (id && repo) {
    run = api(`repos/${repo}/actions/runs/${id}`);
    jobs = api(`repos/${repo}/actions/runs/${id}/jobs?per_page=100`).jobs ?? [];
    const first = firstFailure(run, jobs);
    excerpt = first ? excerptOf(repo, first) : "";
    existing = JSON.parse(gh(["issue", "list", "--repo", repo, "--state", "open", "--label", label, "--limit", "200", "--json", "number,body"]));
  } else {
    console.error("usage: node scripts/slow-failure.mjs <run-id> | --run run.json --jobs jobs.json [--excerpt log.txt] [--existing issues.json] [--dry-run]");
    return 2;
  }
  const decision = decide({ run, jobs, existing, excerpt });
  console.log(`slow failure: ${decision.action}${decision.reason ? ` — ${decision.reason}` : ""}${decision.first ? ` (${decision.first.job} › ${decision.first.step} at minute ${minute(decision.first.elapsedMs)})` : ""}`);
  if (decision.action === "none" || dryRun) {
    if (dryRun && decision.action !== "none") console.log(decision.title ?? `comment on #${decision.issue}`);
    return 0;
  }
  if (decision.action === "comment") {
    gh(["issue", "comment", String(decision.issue), "--repo", repo, "--body", decision.comment]);
    console.log(`slow failure: commented on #${decision.issue}`);
    return 0;
  }
  try {
    gh(["label", "create", label, "--repo", repo, "--color", "B60205", "--description", "a workflow run that failed after its first five minutes; find the check that fails it sooner"], { stdio: "ignore" });
  } catch {
    // It exists.
  }
  const milestone = nextMilestone(repo);
  const args = ["issue", "create", "--repo", repo, "--title", decision.title, "--body", decision.body, "--label", label];
  if (milestone) args.push("--milestone", milestone);
  const url = gh(args).trim();
  console.log(`slow failure: filed ${url}`);
  return 0;
}

if (process.argv[1] && realpathSync(process.argv[1]) === realpathSync(fileURLToPath(import.meta.url))) {
  process.exitCode = await main();
}
