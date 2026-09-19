// The first-publish check against a registry written here, so that the
// answers it is held to include the one no run has produced yet: a name npm
// has never served. The real registry is asked by the release workflow and
// by nothing in this suite.
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { readFileSync } from "node:fs";
import { createServer } from "node:http";
import { join } from "node:path";
import { test } from "node:test";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";
import { packages, root } from "./packages.mjs";

const script = fileURLToPath(new URL("./first-publish.mjs", import.meta.url));

/**
 * A registry that serves the names given and answers `404` — or whatever
 * `status` says — for the rest, on a port the machine chose.
 */
async function registry(t, { serving = [], status = 404 } = {}) {
  const known = new Set(serving);
  const server = createServer((request, response) => {
    const name = decodeURIComponent(request.url.slice(1));
    if (known.has(name)) {
      response.writeHead(200, { "content-type": "application/json" });
      response.end(JSON.stringify({ name, "dist-tags": { latest: "0.4.0" } }));
    } else {
      response.writeHead(status, { "content-type": "application/json" });
      response.end(JSON.stringify({ error: "Not found" }));
    }
  });
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
  // The check reuses one connection for every name, so the sockets are still
  // open when the test ends and `close` alone would wait for them forever.
  t.after(() => {
    server.closeAllConnections();
    return new Promise(resolve => server.close(resolve));
  });
  return `http://127.0.0.1:${server.address().port}`;
}

/**
 * Runs the check; returns what it said and how it exited rather than
 * throwing. It is asynchronous because the registry it is pointed at is this
 * process's own: a synchronous child would block the event loop that has to
 * answer it, and the two would wait for each other.
 */
const run = promisify(execFile);
async function check(url, { env = {}, args = [] } = {}) {
  const options = { cwd: root, encoding: "utf8", env: { ...process.env, NODE_AUTH_TOKEN: "", NPM_TOKEN: "", ...env } };
  try {
    const { stdout } = await run(process.execPath, [script, "--registry", url, ...args], options);
    return { code: 0, out: stdout };
  } catch (error) {
    return { code: error.code, out: (error.stdout ?? "") + (error.stderr ?? "") };
  }
}

/** Every name this workspace publishes, which is what the check discovers. */
const names = packages.map(directory => JSON.parse(readFileSync(join(root, directory, "package.json"), "utf8")).name);

test("every name already served needs nothing provisioned", async t => {
  const url = await registry(t, { serving: names });
  const { code, out } = await check(url, { args: ["--require-credential"] });
  assert.equal(code, 0, out);
  assert.match(out, /already on http:\/\/127\.0\.0\.1/);
  assert.doesNotMatch(out, /first publish/);
});

test("a name the registry has never served is named, and a rehearsal does not need a credential for it", async t => {
  const url = await registry(t, { serving: names.slice(1) });
  const { code, out } = await check(url);
  assert.equal(code, 0, out);
  assert.match(out, new RegExp(`first publish: ${names[0].replace("/", "\/")}`));
  assert.match(out, /no credential is set, which a rehearsal does not need/);
});

test("a first publish with a credential can be made", async t => {
  const url = await registry(t, { serving: names.slice(1) });
  const { code, out } = await check(url, { args: ["--require-credential"], env: { NODE_AUTH_TOKEN: "npm_fixture" } });
  assert.equal(code, 0, out);
  assert.match(out, /1 first publish beside \d+ established names?/);
  assert.match(out, /a credential is set/);
});

test("a first publish with no credential refuses the run, naming it", async t => {
  const url = await registry(t, { serving: names.slice(1) });
  const { code, out } = await check(url, { args: ["--require-credential"] });
  assert.equal(code, 1);
  assert.match(out, /not ready to publish/);
  assert.ok(out.includes(names[0]), out);
  assert.match(out, /trusted publisher to a package that exists/);
});

test("a registry that answers neither 200 nor 404 refuses rather than reading as a new name", async t => {
  const url = await registry(t, { serving: names.slice(1), status: 500 });
  const { code, out } = await check(url);
  assert.equal(code, 1);
  assert.match(out, /the registry answered 500/);
  assert.match(out, /a name nobody could ask about is not a name nobody has published/);
});

test("an unknown flag is refused before the registry is asked", async t => {
  const url = await registry(t, { serving: names });
  const { code, out } = await check(url, { args: ["--publish"] });
  assert.equal(code, 2);
  assert.match(out, /usage: node scripts\/first-publish\.mjs/);
});
