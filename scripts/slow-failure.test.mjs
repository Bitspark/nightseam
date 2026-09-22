// What a slow failure files, on runs no workflow has produced: a run that
// failed at minute eighteen leaves an issue naming the step and the minute;
// one that failed at minute three leaves nothing; a repeat of the same
// failure comments on the open issue; a green or cancelled run is nothing.
import assert from "node:assert/strict";
import { test } from "node:test";
import { budgetMs, decide, firstFailure, marker } from "./slow-failure.mjs";

const at = minutes => new Date(Date.UTC(2026, 8, 22, 10, 20 + minutes)).toISOString();

const run = (overrides = {}) => ({
  id: 35715462290,
  name: "release",
  event: "push",
  head_branch: "v0.6.0",
  head_sha: "5cc9723a24646c40ed1861f892b2b23eb6d785d7",
  html_url: "https://github.com/Bitspark/nightseam/actions/runs/35715462290",
  run_started_at: at(0),
  conclusion: "failure",
  ...overrides,
});

const job = (name, steps, overrides = {}) => ({
  id: 1,
  name,
  conclusion: steps.some(s => s.conclusion === "failure") ? "failure" : "success",
  completed_at: steps.at(-1)?.completed_at,
  steps,
  ...overrides,
});
const step = (name, conclusion, minutes) => ({ name, conclusion, completed_at: at(minutes) });

const slow = [
  job("release", [
    step("Run go test ./...", "success", 18),
    step("Publish to npm", "success", 40),
    step("The round trip against what was published", "failure", 45),
  ]),
];

test("a run that failed after its first five minutes leaves an issue naming the step and the minute", () => {
  const decision = decide({ run: run(), jobs: slow, excerpt: "GET https://registry.npmjs.org/@nightseam%2Fauth: Not Found - 404\n" });
  assert.equal(decision.action, "create");
  assert.equal(decision.first.step, "The round trip against what was published");
  assert.equal(decision.title, "flows: release failed at minute 45 in release › The round trip against what was published; how could it fail in under five?");
  assert.match(decision.body, /^## What\n/);
  assert.match(decision.body, /failed at minute 45: job `release`, step `The round trip against what was published`/);
  assert.match(decision.body, /Not Found - 404/);
  assert.match(decision.body, /## Held by\n\nA check that fails the same defect in under five minutes/);
  assert.match(decision.body, /## Waits on\n\nNothing\./);
  assert.ok(decision.body.trimEnd().endsWith(marker(run(), decision.first)), "the marker ends the body");
  assert.equal(decision.marker, "<!-- slow-failure: release / release / The round trip against what was published -->");
});

test("a run that failed within its first five minutes leaves nothing: it already failed fast", () => {
  const fast = [job("preflight", [step("Hold the versions and the matrix to the tag", "failure", 3)])];
  const decision = decide({ run: run(), jobs: fast });
  assert.equal(decision.action, "none");
  assert.match(decision.reason, /minute 3, within the five-minute budget/);
  assert.equal(budgetMs, 5 * 60_000);
});

test("a green or a cancelled run leaves nothing", () => {
  assert.equal(decide({ run: run({ conclusion: "success" }), jobs: slow }).action, "none");
  assert.equal(decide({ run: run({ conclusion: "cancelled" }), jobs: slow }).action, "none");
  assert.equal(decide({ run: run(), jobs: [job("full", [step("Run go test ./...", "success", 20)])] }).action, "none");
});

test("a repeat of the same failure comments on the open issue rather than filing a second", () => {
  const first = decide({ run: run(), jobs: slow });
  const existing = [{ number: 603, body: `an open issue\n\n${first.marker}\n` }, { number: 7, body: "another" }];
  const again = decide({ run: run({ id: 1, html_url: "https://github.com/Bitspark/nightseam/actions/runs/1" }), jobs: slow, existing, excerpt: "again" });
  assert.equal(again.action, "comment");
  assert.equal(again.issue, 603);
  assert.match(again.comment, /^Failed again at minute 45 in `release` › `The round trip against what was published`: https:\/\/github.com\/Bitspark\/nightseam\/actions\/runs\/1/);
  assert.match(again.comment, /again/);
});

test("the earliest failing step across the run's jobs is the one named, and a job failing at no step is named by the job", () => {
  const jobs = [
    job("full", [step("Run go test ./...", "failure", 30)], { id: 2 }),
    job("swift", [step("Run node scripts/swift.mjs", "failure", 12)], { id: 3 }),
    { id: 4, name: "lost", conclusion: "failure", completed_at: at(9), steps: [] },
  ];
  const first = firstFailure(run(), jobs);
  assert.equal(first.job, "lost");
  assert.equal(first.step, "(the job, at no step)");
  assert.equal(first.jobId, 4);
  assert.equal(Math.round(first.elapsedMs / 60_000), 9);
  const decision = decide({ run: run(), jobs: jobs.slice(0, 2) });
  assert.equal(decision.first.job, "swift");
  assert.equal(decision.first.jobId, 3);
});
