import assert from "node:assert/strict";
import { test } from "node:test";
import { catalogue, holdOutput, selectExamples } from "./examples.mjs";

test("every advertised example has a runnable consumer and documented result", () => {
  const all = catalogue();
  assert.ok(all.some(example => example.id === "rebuild-with-state"));
  assert.ok(all.some(example => example.id === "probe"));
  assert.deepEqual(selectExamples(["check", "--all"], all).examples, all);
  assert.deepEqual(selectExamples(["run", "rebuild-with-state", "--language=go"], all).languages, ["go"]);
  assert.throws(() => selectExamples(["run", "missing"], all), /unknown example/);
  assert.throws(() => selectExamples(["run", "rebuild-with-state", "--language=python"], all), /language/);
  assert.throws(() => selectExamples(["run", "probe", "--language=go"], all), /both languages/);
  assert.throws(() => selectExamples(["check"], all), /usage/);
  assert.throws(() => selectExamples(["check", "--all", "check"], all), /usage/);
  assert.throws(() => selectExamples(["run", "rebuild-with-state", "--language="], all), /language/);
});

test("an example fails on a changed, missing or additional observation", () => {
  holdOutput("items: book, pen\r\n", "items: book, pen\n", "cart");
  for (const actual of ["items: \n", "", "items: book, pen\nextra\n"]) {
    assert.throws(() => holdOutput(actual, "items: book, pen\n", "cart"), /cart/);
  }
});
