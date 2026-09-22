// Whether CI already proved a commit: a `full` check of ci.yml that
// concluded green on the exact SHA, made by a run of that workflow on that
// SHA. The release workflow asks this before its tiers and skips them on
// the proof, naming the run that made it; on a commit with no such check —
// one no pull request carried — it runs them as before. The operator's
// verdict A on #618; RELEASING.md, *Cutting a release*.
//
//	node scripts/tiers-proven.mjs <sha>                                   # with GH_TOKEN and GH_REPO
//	node scripts/tiers-proven.mjs <sha> --checks checks.json --runs runs.json   # from files, for a test
//
// It prints `proven=true` and `proof=<url>`, or `proven=false` and
// `reason=…`, one per line, for the step to append to $GITHUB_OUTPUT, and
// says the same in words on stderr. The decision is a function of the
// check runs and the runs they name, so that a test holds it on checks no
// commit has carried; the script runs only when it is the program.
import { readFileSync, realpathSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

/** The workflow and the job whose green check is the proof. */
export const workflowPath = ".github/workflows/ci.yml";
export const checkName = "full";

/** The run a check run's page belongs to, read off its URL. */
export function runIdOf(url) {
  const match = /\/actions\/runs\/(\d+)\b/.exec(url ?? "");
  return match ? match[1] : undefined;
}

/**
 * The proof, or why there is none. `checkRuns` are the commit's check runs
 * as the API lists them; `runOf(id)` is the workflow run a check names, or
 * undefined. A candidate is ci.yml's `full`, completed and green, made by
 * GitHub Actions; it proves the commit when its run is of that workflow
 * and of that SHA — a `full` job of another workflow, or a run whose head
 * is another commit, proves nothing here.
 */
export function proof(sha, checkRuns, runOf) {
  const candidates = (checkRuns ?? []).filter(
    check =>
      check.name === checkName &&
      check.status === "completed" &&
      check.conclusion === "success" &&
      (check.app?.slug ?? "github-actions") === "github-actions",
  );
  let seen = 0;
  for (const check of candidates) {
    const id = runIdOf(check.html_url ?? check.details_url);
    if (!id) continue;
    const run = runOf(id);
    if (!run) continue;
    seen++;
    if (run.path !== workflowPath || run.head_sha !== sha || run.conclusion !== "success") continue;
    return { proven: true, url: check.html_url ?? check.details_url, run: id };
  }
  if (candidates.length === 0) return { proven: false, reason: `no green ${checkName} check on ${sha.slice(0, 8)}` };
  if (seen === 0) return { proven: false, reason: `a green ${checkName} check on ${sha.slice(0, 8)} names no run that could be read` };
  return { proven: false, reason: `a green ${checkName} check on ${sha.slice(0, 8)} is not ${workflowPath}'s run of that commit` };
}

/** The lines a step appends to $GITHUB_OUTPUT. */
export function outputs(result) {
  return result.proven ? `proven=true\nproof=${result.url}\n` : `proven=false\nreason=${result.reason}\n`;
}

const gh = args => execFileSync("gh", args, { encoding: "utf8", stdio: ["ignore", "pipe", "inherit"] });

export async function main(argv = process.argv.slice(2)) {
  const flag = name => {
    const at = argv.indexOf(name);
    return at < 0 ? undefined : argv[at + 1];
  };
  const sha = argv.find(argument => /^[0-9a-f]{40}$/.test(argument));
  if (!sha) {
    console.error("usage: node scripts/tiers-proven.mjs <full sha> [--checks checks.json --runs runs.json]");
    return 2;
  }
  let checkRuns;
  let runOf;
  if (flag("--checks")) {
    const listed = JSON.parse(readFileSync(flag("--checks"), "utf8"));
    checkRuns = Array.isArray(listed) ? listed : listed.check_runs;
    const runs = flag("--runs") ? JSON.parse(readFileSync(flag("--runs"), "utf8")) : {};
    runOf = id => runs[id];
  } else {
    const repo = process.env.GH_REPO;
    if (!repo) {
      console.error("GH_REPO is not set; the check runs are the repository's");
      return 2;
    }
    checkRuns = JSON.parse(gh(["api", "--paginate", "--slurp", `repos/${repo}/commits/${sha}/check-runs?per_page=100`])).flatMap(page => page.check_runs ?? []);
    const cache = new Map();
    runOf = id => {
      if (!cache.has(id)) {
        try {
          const run = JSON.parse(gh(["api", `repos/${repo}/actions/runs/${id}`]));
          cache.set(id, { path: run.path, head_sha: run.head_sha, conclusion: run.conclusion });
        } catch {
          cache.set(id, undefined);
        }
      }
      return cache.get(id);
    };
  }
  const result = proof(sha, checkRuns, runOf);
  process.stdout.write(outputs(result));
  console.error(result.proven ? `CI proved ${sha.slice(0, 8)}: ${result.url}; the tiers are not run again` : `CI has not proved ${sha.slice(0, 8)} — ${result.reason}; the tiers run here`);
  return 0;
}

if (process.argv[1] && realpathSync(process.argv[1]) === realpathSync(fileURLToPath(import.meta.url))) {
  process.exitCode = await main();
}
