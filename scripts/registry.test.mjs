import assert from "node:assert/strict";
import { once } from "node:events";
import { readFileSync } from "node:fs";
import { createServer } from "node:http";
import { join } from "node:path";
import { test } from "node:test";
import { packages, root } from "./packages.mjs";
import { waitForRegistries } from "./registry.mjs";

const tag = "v0.4.0";
const version = tag.slice(1);
const runtime = "/npm/@nightseam/runtime";
const core = `/go/github.com/!bitspark/nightseam/@v/${tag}.info`;
const adapter = `/go/github.com/!bitspark/nightseam/otel/go/@v/${tag}.info`;
const manifest = { "dist-tags": { latest: version } };
const info = { Version: tag, Time: "2026-09-19T00:00:00Z" };

async function registry(t, respond) {
  const calls = new Map();
  const server = createServer((req, res) => {
    const path = decodeURIComponent(req.url);
    const count = (calls.get(path) ?? 0) + 1;
    calls.set(path, count);
    respond(path, count, res);
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  t.after(async () => {
    const closed = new Promise(resolve => server.close(resolve));
    server.closeAllConnections();
    await closed;
  });
  const base = `http://127.0.0.1:${server.address().port}`;
  const messages = [];
  return {
    calls,
    messages,
    options: { npmRegistry: `${base}/npm`, goProxy: `${base}/go`, log: message => messages.push(message) },
  };
}

function ready(path, res) {
  res.setHeader("Content-Type", "application/json");
  res.end(JSON.stringify(path.startsWith("/npm/") ? manifest : info));
}

test("the release waits through missing manifests and stale versions in both registries", { timeout: 15_000 }, async t => {
  const fake = await registry(t, (path, count, res) => {
    if ((path === runtime || path === core) && count === 1) {
      res.writeHead(404).end();
    } else if (path === runtime && count === 2) {
      res.end(JSON.stringify({ "dist-tags": { latest: "0.3.0" } }));
    } else if (path === adapter && count === 1) {
      res.end(JSON.stringify({ Version: "v0.3.0" }));
    } else {
      ready(path, res);
    }
  });
  await waitForRegistries(tag, fake.options);
  for (const directory of packages) {
    const { name } = JSON.parse(readFileSync(join(root, directory, "package.json"), "utf8"));
    assert.ok(fake.calls.has(`/npm/${name}`), `${name} was not checked`);
  }
  assert.ok(fake.calls.get(runtime) >= 3, "a stale latest must not pass");
  assert.ok(fake.calls.get(core) >= 2, "the root module must propagate too");
  assert.ok(fake.calls.get(adapter) >= 2, "the nested module must serve the requested version");
  assert.match(fake.messages.join("\n"), /available after [\d.]+s/);
});

test("a propagation timeout names every package still unavailable and the last response", { timeout: 5_000 }, async t => {
  const fake = await registry(t, (path, count, res) => {
    if (path === runtime) res.writeHead(404).end();
    else if (path === adapter) res.end(JSON.stringify({ Version: "v0.3.0" }));
    else ready(path, res);
  });
  await assert.rejects(waitForRegistries(tag, { ...fake.options, timeoutMs: 500 }), error => {
    assert.match(error.message, /registry propagation timed out after [\d.]+s/);
    assert.match(error.message, /@nightseam\/runtime@0\.4\.0.*HTTP 404/);
    assert.match(error.message, /github\.com\/Bitspark\/nightseam\/otel\/go@v0\.4\.0.*v0\.3\.0/);
    return true;
  });
});

test("an early timer wakeup does not start another request at the deadline", async t => {
  const fake = await registry(t, (path, count, res) => {
    if (path === runtime) res.writeHead(404).end();
    else ready(path, res);
  });
  let now = 0;
  let firstSleep = true;
  const clock = {
    now: () => now,
    sleep: async milliseconds => {
      // Timers truncate fractional milliseconds. The first wakeup is early;
      // waiting for the remaining fraction must not send another request.
      now += firstSleep ? milliseconds - 0.5 : milliseconds;
      firstSleep = false;
    },
  };
  await assert.rejects(waitForRegistries(tag, { ...fake.options, timeoutMs: 500, clock }), /registry propagation timed out.*HTTP 404/s);
  assert.equal(fake.calls.get(runtime), 1, "the final backoff must reach its deadline before another request can start");
});

for (const phase of ["headers", "body"]) {
  test(`the deadline also bounds a registry that stalls its ${phase}`, { timeout: 5_000 }, async t => {
    const fake = await registry(t, (path, count, res) => {
      if (path !== runtime) ready(path, res);
      else if (phase === "body") {
        res.writeHead(200, { "Content-Type": "application/json" });
        res.write('{"dist-tags":');
      }
    });
    await assert.rejects(waitForRegistries(tag, { ...fake.options, timeoutMs: 500 }), /registry propagation timed out.*\n.*@nightseam\/runtime/s);
  });
}

test("transient connection errors and malformed responses can recover", { timeout: 15_000 }, async t => {
  const fake = await registry(t, (path, count, res) => {
    if (path === runtime && count === 1) res.destroy();
    else if (path === adapter && count === 1) res.end("not yet JSON");
    else if (path === core && count === 1) res.writeHead(503).end();
    else ready(path, res);
  });
  await waitForRegistries(tag, fake.options);
  for (const path of [runtime, adapter, core]) assert.ok(fake.calls.get(path) >= 2);
});
