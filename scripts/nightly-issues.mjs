// Turns a nightly full-matrix run into issues: one per scenario that failed,
// titled by the scenario, with every pairing it failed in named in the body.
// Group by scenario so its diagnostics can be compared across pairings.
// The run alone does not establish whether a runtime, driver, or scenario
// caused a failure, nor whether either participant passed against Go.
//
//	go test -json ./conformance/go -run TestMatrix > run.json   # NIGHTSEAM_MATRIX=1
//	node scripts/nightly-issues.mjs run.json --repo owner/name --run-url https://…
//
// --dry-run prints what it would open or update and calls nothing. An issue
// is never closed here: a scenario that stops failing is one somebody fixed,
// and closing its issue is part of fixing it.
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

/** The label every issue this opens carries, so a sweep can find them. */
export const marker = "<!-- nightly-matrix -->";

/**
 * The failures of a `go test -json` stream, by scenario. A subtest is named
 * `TestMatrix/<a>-with-<b>/<layer>/<scenario>` by the runner, or
 * `TestSelf/<layer>/<scenario>` where both sides are the reference; only the
 * leaves are failures of a scenario, the parents failing because a leaf did.
 *
 * Go writes a subtest's name with its spaces as underscores, and a scenario's
 * name is prose. It is left as Go spelled it: turning them back would guess
 * at the underscores a name carries of its own.
 */
export function failures(stream) {
  const output = new Map();
  const failed = new Map();
  for (const line of stream.split("\n")) {
    if (!line.startsWith("{")) continue;
    let event;
    try {
      event = JSON.parse(line);
    } catch {
      continue; // A line of the toolchain's own, not of the test's.
    }
    if (!event.Test) continue;
    if (event.Action === "output") {
      const held = output.get(event.Test) ?? [];
      held.push(event.Output);
      output.set(event.Test, held);
    }
    if (event.Action !== "fail") continue;
    const parts = event.Test.split("/");
    const [pairing, layer, scenario] =
      parts.length === 4 ? [parts[1], parts[2], parts[3]] : parts.length === 3 ? ["go-with-go", parts[1], parts[2]] : [];
    if (!scenario) continue;
    const key = `${layer}/${scenario}`;
    const held = failed.get(key) ?? { layer, scenario, pairings: [] };
    held.pairings.push({ pairing, output: (output.get(event.Test) ?? []).join("") });
    failed.set(key, held);
  }
  for (const held of failed.values()) held.pairings.sort((a, b) => a.pairing.localeCompare(b.pairing));
  return [...failed.values()].sort((a, b) => `${a.layer}/${a.scenario}`.localeCompare(`${b.layer}/${b.scenario}`));
}

/** The title an issue keeps across runs: the scenario, and nothing that moves. */
export const title = failure => `conformance: ${failure.layer}/${failure.scenario} fails the nightly matrix`;

/** What the issue says: which pairings, what each said, and where the run is. */
export function body(failure, runUrl) {
  const lines = [
    marker,
    "",
    `The nightly full matrix — every language against every other — failed \`${failure.layer}/${failure.scenario}\` in ${failure.pairings.length} pairing${failure.pairings.length > 1 ? "s" : ""}.`,
    "",
    "These diagnostics need investigation in the runtime, driver, or scenario. This run does not establish how either participant behaved against the Go reference. CI may remain green with provisional language failures; compare the actual scenario results before assigning a cause.",
    "",
    "| pairing |",
    "| --- |",
    ...failure.pairings.map(p => `| \`${p.pairing}\` |`),
    "",
  ];
  if (runUrl) lines.push(`The run: ${runUrl}`, "");
  for (const p of failure.pairings) {
    if (!p.output.trim()) continue;
    lines.push(`<details><summary><code>${p.pairing}</code></summary>`, "", "```", p.output.trimEnd().slice(-4000), "```", "", "</details>", "");
  }
  lines.push("Opened by `scripts/nightly-issues.mjs`; the body is rewritten by each nightly run that still fails. Close it with the fix.");
  return lines.join("\n");
}

const flag = name => {
  const at = process.argv.indexOf(name);
  return at < 0 ? undefined : process.argv[at + 1];
};

const gh = (repo, args) => execFileSync("gh", [...args, "--repo", repo], { encoding: "utf8" });

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const file = process.argv[2];
  const repo = flag("--repo") ?? process.env.GITHUB_REPOSITORY;
  const runUrl = flag("--run-url");
  const dryRun = process.argv.includes("--dry-run");
  if (!file || (!repo && !dryRun)) {
    console.error("usage: node scripts/nightly-issues.mjs <go test -json output> --repo owner/name [--run-url url] [--dry-run]");
    process.exit(2);
  }
  const found = failures(readFileSync(file, "utf8"));
  if (found.length === 0) {
    console.log("the full matrix is green: no issue to open");
    process.exit(0);
  }
  // Every open issue once, matched by exact title: `gh issue list --search`
  // is a fuzzy search, and an issue opened twice for one scenario is worse
  // than one opened late.
  const open = dryRun ? [] : JSON.parse(gh(repo, ["issue", "list", "--state", "open", "--limit", "200", "--json", "number,title"]));
  for (const failure of found) {
    const name = title(failure);
    const text = body(failure, runUrl);
    const existing = open.find(issue => issue.title === name);
    if (dryRun) {
      console.log(`${existing ? "update" : "open"}: ${name}`);
      continue;
    }
    if (existing) {
      gh(repo, ["issue", "edit", String(existing.number), "--body", text]);
      console.log(`updated #${existing.number}: ${name}`);
    } else {
      const url = gh(repo, ["issue", "create", "--title", name, "--body", text]).trim();
      console.log(`opened ${url}: ${name}`);
    }
  }
  console.log(`${found.length} scenario${found.length > 1 ? "s" : ""} failing across the matrix`);
}
