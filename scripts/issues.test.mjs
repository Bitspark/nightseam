import assert from "node:assert/strict";
import { test } from "node:test";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import { audit, field, prerequisites, readLive } from "./issues.mjs";

const laneBody = `### What
Make the change.
### Surface
None.
### Held by
TestTheChange.
### Provenance
#1.
### Waits on
Nothing.
### Touches
\`scripts\`
`;
const issue = (number, extra = {}) => ({ number, title: `Issue ${number}`, state: "open", type: "Task",
  milestone: { title: "0.5.0", state: "open" }, labels: [], body: laneBody,
  parent: 1, blockedBy: [], comments: [], ...extra });
const epic = issue(1, { type: "Epic", body: "## What\nThe whole.", parent: null });
const snapshot = (...issues) => ({ version: 1, repository: "Bitspark/nightseam", issues: [epic, ...issues] });
const codes = report => report.findings.map(finding => finding.code);

test("an active lane with its Epic and required fields passes", () => {
  const report = audit(snapshot(issue(2)));
  assert.deepEqual(report.findings, []);
  assert.equal(report.active, 2);
});

test("missing milestone, type and parent are findings with issue URLs", () => {
  const report = audit(snapshot(issue(2, { milestone: null, type: null, parent: null })));
  assert.deepEqual(codes(report).sort(), ["missing-milestone", "missing-parent", "missing-type"]);
  assert.ok(report.findings.every(f => f.number === 2 && f.url === "https://github.com/Bitspark/nightseam/issues/2"));
});

test("a lane parent must be an Epic and parent records must be present", () => {
  assert.ok(codes(audit(snapshot(issue(2), issue(3, { parent: 2 })))).includes("parent-not-epic"));
  assert.ok(codes(audit(snapshot(issue(2, { parent: 99 })))).includes("missing-reference"));
});

test("hierarchy and active dependency cycles are reported separately", () => {
  const hierarchy = snapshot(issue(2, { type: "Epic", parent: 3 }), issue(3, { type: "Epic", parent: 2 }));
  assert.ok(codes(audit(hierarchy)).includes("parent-cycle"));
  const wait = n => laneBody.replace("Nothing.", `#${n}.`);
  assert.ok(codes(audit(snapshot(issue(2, { body: wait(3), blockedBy: [3] }), issue(3, { body: wait(2), blockedBy: [2] })))).includes("dependency-cycle"));
});

test("Task and Bug lanes require all six fields, including the bug-form What label", () => {
  for (const type of ["Task", "Bug"]) {
    const report = audit(snapshot(issue(2, { type, body: "## What\nFix it." })));
    assert.equal(report.findings.filter(f => f.code === "missing-field").length, 5);
  }
  assert.deepEqual(audit(snapshot(issue(2, { type: "Bug", body: laneBody.replace("### What", "### What happens, and what should happen instead") }))).findings, []);
});

test("headings and bold fields delimit only their own content, including adjacent inline fields", () => {
  const body = "**What:** A change.\n**Surface** None.\n**Held by:** TestIt.\n**Provenance:** #1.\n**Waits on** #3, #4. **Touches:** `scripts`\n";
  assert.equal(field(body, "waits on"), "#3, #4.");
  assert.equal(field(body, "touches"), "`scripts`");
  assert.deepEqual(prerequisites(body).numbers, [3, 4]);
  assert.equal(field("```md\n## What\nFake\n```\n## What\nReal", "what"), "Real");
});

test("only explicit Waits on lists create dependencies; explanations and contextual references do not", () => {
  const body = `## Provenance\n#90\n## Waits on\n- #3, #4 and #5 must land first (the context is #80).\n- #6 — described in #81.\n\nThis follows the research in #82.\n## Dependency notes\nCoordinate with #7.\n`;
  assert.deepEqual(prerequisites(body).numbers, [3, 4, 5, 6]);
  assert.deepEqual(prerequisites("## Waits on\nNothing. Coordinate with #8.").numbers, []);
  assert.deepEqual(prerequisites("## Waits on / Unblocks\n#9 unblocks #10").numbers, []);
  assert.equal(prerequisites("## Waits on\nAfter the decision in #9.").ambiguous, true);
});

test("a concrete prerequisite needs its native edge, including a satisfied closed prerequisite", () => {
  const body = laneBody.replace("Nothing.", "#3 — already landed.");
  const done = issue(3, { state: "closed", body: "Old issue without current fields", parent: null, milestone: null, type: null });
  assert.ok(codes(audit(snapshot(issue(2, { body }), done))).includes("missing-dependency"));
  const report = audit(snapshot(issue(2, { body, blockedBy: [3] }), done));
  assert.deepEqual(report.findings, []);
  assert.deepEqual(report.dependencies, [{ number: 2, prerequisite: 3, state: "closed" }]);
});

test("native blockers absent from an explicit Waits on field and ambiguous fields are visible", () => {
  assert.ok(codes(audit(snapshot(issue(2, { blockedBy: [3] }), issue(3)))).includes("unlisted-dependency"));
  assert.ok(codes(audit(snapshot(issue(2, { body: laneBody.replace("Nothing.", "After the decision in #3.") }), issue(3)))).includes("ambiguous-waits-on"));
});

test("unresolved design questions are exempt from implementation fields and are not structural failures", () => {
  const design = issue(2, { labels: ["design"], body: "## The question\nChoose one.\n## Verdict\n_No response_\n## Waits on / Unblocks\n#3 is context." });
  const report = audit(snapshot(design));
  assert.deepEqual(report.findings, []);
  assert.equal(report.designs[0].verdict, "not-recorded");
});

test("a recorded design verdict does not hide its unfinished downstream implementation", () => {
  const design = issue(2, { labels: ["design"], body: "## The question\nChoose one.", comments: [
    { body: "My recommendation is A; not an operator verdict.", url: "https://example.test/proposal" },
    { body: "## Verdict\nThe operator selected A.\n## Next\nImplementation follows.", url: "https://example.test/verdict" },
  ] });
  const lane = issue(3, { body: laneBody.replace("Nothing.", "#2."), blockedBy: [2] });
  const report = audit(snapshot(design, lane));
  assert.deepEqual(report.findings, []);
  assert.deepEqual(report.designs[0], { number: 2, verdict: "recorded", source: "https://example.test/verdict", downstream: [{ number: 3, state: "open" }] });
  assert.equal(audit(snapshot({ ...design, comments: design.comments.slice(0, 1) })).designs[0].verdict, "not-recorded");
  const closed = audit(snapshot({ ...design, state: "closed", body: "Historical decision" }, lane));
  assert.deepEqual(closed.findings, []);
  assert.equal(closed.designs[0].verdict, "recorded");
  assert.deepEqual(closed.designs[0].downstream, [{ number: 3, state: "open" }]);
});

test("closed historical issues, closed milestones and pull requests do not become active failures", () => {
  const historical = issue(2, { state: "closed", type: null, body: "", parent: null });
  const closedMilestone = issue(3, { milestone: { title: "old", state: "closed" }, type: null, body: "", parent: null });
  assert.deepEqual(audit(snapshot(historical, closedMilestone)).findings, []);
});

test("incomplete snapshots cannot silently pass as an empty plan", () => {
  assert.throws(() => audit({}), /snapshot/);
  const incomplete = snapshot(issue(2));
  delete incomplete.issues[1].blockedBy;
  assert.throws(() => audit(incomplete), /blockedBy/);
  assert.throws(() => audit(snapshot(issue(2), issue(2))), /duplicate/);
});

test("live loading excludes PRs, loads closed dependencies and parents, and never writes GitHub", async () => {
  const raw = (n, extra = {}) => ({ number: n, title: `Issue ${n}`, state: "open", type: { name: "Task" }, milestone: { title: "next", state: "open" }, labels: [], body: laneBody, ...extra });
  const calls = [];
  const api = async (path, options = {}) => {
    calls.push([path, options]);
    if (path.includes("issues?")) return [raw(2, { body: laneBody.replace("Nothing.", "#3.") }), { number: 99, pull_request: {} }];
    path = path.split("?")[0];
    if (path.endsWith("/2/parent")) return raw(1, { type: { name: "Epic" }, body: "" });
    if (path.endsWith("/2/dependencies/blocked_by")) return [raw(3, { state: "closed", body: "old" })];
    if (path.endsWith("/1/parent")) return null;
    if (path.endsWith("/1/dependencies/blocked_by")) return [];
    throw new Error(`unexpected read ${path}`);
  };
  const data = await readLive("Bitspark/nightseam", api);
  assert.deepEqual(audit(data).findings, []);
  assert.deepEqual(data.issues.map(i => i.number).sort(), [1, 2, 3]);
  assert.ok(calls.every(([path]) => path.startsWith("repos/Bitspark/nightseam/issues")));
  await assert.rejects(readLive("Bitspark/nightseam", async () => { throw new Error("permission denied"); }), /permission denied/);
});

test("the recorded JSON CLI reports findings and exits nonzero without GitHub access", () => {
  const dir = mkdtempSync(join(tmpdir(), "nightseam-issue-audit-"));
  try {
    const input = join(dir, "plan.json"), output = join(dir, "report.json");
    writeFileSync(input, JSON.stringify(snapshot(issue(2, { parent: null }))));
    const result = spawnSync(process.execPath, ["scripts/issues.mjs", "check", "--input", input, "--json", output], { encoding: "utf8" });
    assert.equal(result.status, 1, result.stderr);
    assert.match(result.stdout, /\[#2\]\(https:\/\/github.com\/Bitspark\/nightseam\/issues\/2\)/);
    assert.ok(codes(JSON.parse(readFileSync(output, "utf8"))).includes("missing-parent"));
    const invalid = spawnSync(process.execPath, ["scripts/issues.mjs", "check", "--unknown"], { encoding: "utf8" });
    assert.equal(invalid.status, 2);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
