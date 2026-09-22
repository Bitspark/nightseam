// What the nightly reads out of a run, over a `go test -json` stream written
// here. The stream is the shape the toolchain emits: an event per line, the
// parents failing because their leaves did, and a scenario's name spelled
// with its spaces as underscores.
import assert from "node:assert/strict";
import { test } from "node:test";
import { body, failures, marker, title } from "./nightly-issues.mjs";

const event = (Action, Test, Output) => JSON.stringify({ Time: "2026-09-19T00:00:00Z", Action, Package: "github.com/Bitspark/nightseam/conformance/go", Test, Output });

const stream = [
  event("run", "TestMatrix"),
  event("run", "TestMatrix/python-with-rust"),
  event("run", "TestMatrix/python-with-rust/tunnel/open_refused"),
  event("output", "TestMatrix/python-with-rust/tunnel/open_refused", "    suite.go:140: conformance/scenarios/tunnel/open-refused.json (a: python, b: rust)\n"),
  event("output", "TestMatrix/python-with-rust/tunnel/open_refused", "        step 7, tunnel.open on b: the answer is not the one expected\n"),
  event("fail", "TestMatrix/python-with-rust/tunnel/open_refused"),
  event("run", "TestMatrix/rust-with-python/tunnel/open_refused"),
  event("fail", "TestMatrix/rust-with-python/tunnel/open_refused"),
  event("run", "TestMatrix/rust-with-python/peer/a_call_and_a_call_back"),
  event("fail", "TestMatrix/rust-with-python/peer/a_call_and_a_call_back"),
  event("pass", "TestMatrix/python-with-rust/tunnel/credit_stall"),
  // The parents fail because their leaves did; neither is a scenario.
  event("fail", "TestMatrix/python-with-rust"),
  event("fail", "TestMatrix"),
  "ok  \tgithub.com/Bitspark/nightseam/conformance/go\t41.2s",
  "",
].join("\n");

test("a scenario is one failure however many pairings tripped over it", () => {
  const found = failures(stream);
  assert.equal(found.length, 2);
  assert.deepEqual(
    found.map(f => `${f.layer}/${f.scenario}`),
    ["peer/a_call_and_a_call_back", "tunnel/open_refused"],
  );
  const gate = found.find(f => f.scenario === "open_refused");
  assert.deepEqual(gate.pairings.map(p => p.pairing), ["python-with-rust", "rust-with-python"]);
});

test("the parents that failed because a leaf did are not scenarios", () => {
  const found = failures(stream);
  assert.ok(!found.some(f => f.scenario === undefined || f.layer === "python-with-rust"));
});

test("a run of the reference against itself is named as such, its subtests being one segment shorter", () => {
  const self = [event("run", "TestSelf/seam/order_and_whole"), event("fail", "TestSelf/seam/order_and_whole")].join("\n");
  const [found] = failures(self);
  assert.equal(found.layer, "seam");
  assert.deepEqual(found.pairings.map(p => p.pairing), ["go-with-go"]);
});

test("a green run yields nothing", () => {
  assert.deepEqual(failures([event("run", "TestMatrix"), event("pass", "TestMatrix")].join("\n")), []);
});

test("what is not an event of the toolchain is passed over rather than thrown on", () => {
  assert.deepEqual(failures("not json\n{broken\n\n"), []);
});

test("the title is the scenario and nothing that moves between runs", () => {
  const [found] = failures(stream);
  assert.equal(title(found), "conformance: peer/a_call_and_a_call_back fails the nightly matrix");
  // Two runs of the same failure name the same issue, which is what makes an
  // update an update.
  assert.equal(title(failures(stream)[0]), title(found));
});

test("the body names every pairing, carries what each said, without claiming an unobserved cause", () => {
  const gate = failures(stream).find(f => f.scenario === "open_refused");
  const text = body(gate, "https://github.com/Bitspark/nightseam/actions/runs/1");
  assert.match(text, /^<!-- nightly-matrix -->/);
  assert.ok(text.includes(marker));
  assert.match(text, /\| `python-with-rust` \|/);
  assert.match(text, /\| `rust-with-python` \|/);
  assert.doesNotMatch(text, /it is green|the reference tolerates|both languages pass against Go/);
  assert.match(text, /runtime, driver, or scenario/);
  assert.match(text, /step 7, tunnel\.open on b/);
  assert.match(text, /actions\/runs\/1/);
});

test("a pairing that said nothing carries no empty block", () => {
  const [found] = failures([event("run", "TestMatrix/a-with-b/peer/quiet"), event("fail", "TestMatrix/a-with-b/peer/quiet")].join("\n"));
  assert.doesNotMatch(body(found, undefined), /<details>/);
});
