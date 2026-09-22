// What proves a commit to the release workflow, on check runs no commit has
// carried: ci.yml's `full`, completed and green, made by a run of ci.yml on
// that SHA — and nothing else: a red, cancelled or running one, a check of
// another name, a `full` job of another workflow, a run whose head is
// another commit, or no checks at all.
import assert from "node:assert/strict";
import { test } from "node:test";
import { checkName, outputs, proof, runIdOf, workflowPath } from "./tiers-proven.mjs";

const sha = "5cc9723a24646c40ed1861f892b2b23eb6d785d7";
const other = "f9d4ee0354f9af0a25f6993c6bbb24a6b8e8d2ba";
const url = (run, job) => `https://github.com/Bitspark/nightseam/actions/runs/${run}/job/${job}`;
const check = (name, conclusion, run, overrides = {}) => ({
  name,
  status: conclusion === null ? "in_progress" : "completed",
  conclusion,
  app: { slug: "github-actions" },
  html_url: url(run, run * 3 + 1),
  ...overrides,
});
const runs = {
  100: { path: workflowPath, head_sha: sha, conclusion: "success" },
  200: { path: ".github/workflows/nightly.yml", head_sha: sha, conclusion: "success" },
  300: { path: workflowPath, head_sha: other, conclusion: "success" },
};
const runOf = id => runs[id];

test("a green full check of ci.yml on the commit proves it, and the proof names the run", () => {
  const result = proof(sha, [check("fast (ubuntu-latest)", "success", 100), check("full", "success", 100), check("release", "failure", 900)], runOf);
  assert.deepEqual(result, { proven: true, url: url(100, 301), run: "100" });
  assert.equal(outputs(result), `proven=true\nproof=${url(100, 301)}\n`);
  assert.equal(checkName, "full");
});

test("a red, cancelled or running full check proves nothing", () => {
  for (const conclusion of ["failure", "cancelled", "skipped", "timed_out", null]) {
    const result = proof(sha, [check("full", conclusion, 100)], runOf);
    assert.equal(result.proven, false, String(conclusion));
    assert.match(result.reason, /no green full check/);
  }
});

test("a check of another name, another app, or no checks at all proves nothing", () => {
  assert.equal(proof(sha, [check("fast (windows-latest)", "success", 100), check("swift", "success", 100)], runOf).proven, false);
  assert.equal(proof(sha, [check("full", "success", 100, { app: { slug: "some-other-app" } })], runOf).proven, false);
  assert.equal(proof(sha, [], runOf).proven, false);
  assert.equal(proof(sha, undefined, runOf).proven, false);
  assert.match(outputs(proof(sha, [], runOf)), /^proven=false\nreason=no green full check on 5cc9723a\n$/);
});

test("a full job of another workflow, or a run of another commit, proves nothing", () => {
  const nightly = proof(sha, [check("full", "success", 200)], runOf);
  assert.equal(nightly.proven, false);
  assert.match(nightly.reason, /is not \.github\/workflows\/ci\.yml's run of that commit/);
  const elsewhere = proof(sha, [check("full", "success", 300)], runOf);
  assert.equal(elsewhere.proven, false);
  const unreadable = proof(sha, [check("full", "success", 400)], runOf);
  assert.equal(unreadable.proven, false);
  assert.match(unreadable.reason, /names no run that could be read/);
});

test("the first proving check wins over the ones that prove nothing, whatever the order", () => {
  const result = proof(sha, [check("full", "success", 200), check("full", "success", 300), check("full", "success", 100)], runOf);
  assert.equal(result.proven, true);
  assert.equal(result.run, "100");
});

test("a run is read off the check's page, and a page naming none is no run", () => {
  assert.equal(runIdOf("https://github.com/Bitspark/nightseam/actions/runs/35736897565/job/106776992643"), "35736897565");
  assert.equal(runIdOf("https://github.com/Bitspark/nightseam/pull/620"), undefined);
  assert.equal(runIdOf(undefined), undefined);
});
