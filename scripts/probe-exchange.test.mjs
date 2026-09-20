import assert from "node:assert/strict";
import { test } from "node:test";
import { holdProbeExchange } from "./probe-exchange.mjs";

const exchange = ["echo    -> olleh", "changed -> hello", "notice  -> demo", "stopped -> demo done"];

for (const newline of ["\n", "\r\n"]) {
  test(`the complete installed exchange passes with ${JSON.stringify(newline)} line endings`, () => {
    const output = ["> installed-probe start", "", ...exchange, ""].join(newline);
    assert.doesNotThrow(() => holdProbeExchange(output));
  });
}

for (const expected of exchange) {
  test(`an installed exchange missing ${JSON.stringify(expected)} fails`, () => {
    const output = exchange.filter(line => line !== expected).join("\n");
    assert.throws(() => holdProbeExchange(output), error => error.message.includes(JSON.stringify(expected)));
  });

  test(`an incorrect result starting with ${JSON.stringify(expected)} fails`, () => {
    const output = exchange.map(line => line === expected ? line + " incorrect" : line).join("\n");
    assert.throws(() => holdProbeExchange(output), error => error.message.includes(JSON.stringify(expected)));
  });
}
