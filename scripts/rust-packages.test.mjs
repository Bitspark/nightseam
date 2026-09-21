import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { root } from "./packages.mjs";
import { rustPackages, rustVersionProblems, updateRustVersions } from "./rust-packages.mjs";

test("Rust packages and their local dependencies follow the repository version", () => {
  const version = JSON.parse(readFileSync(join(root, "runtime/ts/package.json"), "utf8")).version;
  const names = rustPackages(root).map(pkg => pkg.name);
  assert.ok(names.includes("nightseam-duplex") && names.includes("nightseam"));
  assert.ok(!names.includes("nightseam-testee"));
  assert.deepEqual(rustVersionProblems(root, version), []);
});

test("a version change moves public and private local crates without rewriting external dependencies", t => {
  const directory = mkdtempSync(join(tmpdir(), "nightseam-rust-version-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const files = {
    "Cargo.toml": '[workspace]\nmembers = ["duplex/rs", "runtime/rs", "conformance/rust"]\n[workspace.package]\nversion = "0.5.0"\n',
    "duplex/rs/Cargo.toml": '[package]\nname = "nightseam-duplex"\nversion.workspace = true\n',
    "runtime/rs/Cargo.toml": '[package]\nname = "nightseam"\nversion.workspace = true\n[dependencies]\nnightseam-duplex = { path = "../../duplex/rs", version = "0.5.0" }\ntokio = "1.47"\n',
    "conformance/rust/Cargo.toml": '[package]\nname = "nightseam-testee"\nversion.workspace = true\npublish = false\n[dependencies]\nnightseam = { path = "../../runtime/rs", version = "0.5.0" }\n',
    "Cargo.lock": 'version = 4\n\n[[package]]\nname = "nightseam"\nversion = "0.5.0"\n\n[[package]]\nname = "nightseam-testee"\nversion = "0.5.0"\n\n[[package]]\nname = "tokio"\nversion = "1.47.0"\nsource = "registry+https://github.com/rust-lang/crates.io-index"\n',
  };
  for (const [file, source] of Object.entries(files)) {
    mkdirSync(join(directory, file, ".."), { recursive: true });
    writeFileSync(join(directory, file), source);
  }
  assert.equal(rustPackages(directory).length, 2);
  assert.ok(rustVersionProblems(directory, "0.6.0").some(problem => problem.includes("Cargo.toml")));
  updateRustVersions(directory, "0.6.0");
  assert.deepEqual(rustVersionProblems(directory, "0.6.0"), []);
  assert.match(readFileSync(join(directory, "runtime/rs/Cargo.toml"), "utf8"), /tokio = "1.47"/);
  assert.match(readFileSync(join(directory, "conformance/rust/Cargo.toml"), "utf8"), /nightseam = \{ path = .*version = "0.6.0"/);
  assert.match(readFileSync(join(directory, "Cargo.lock"), "utf8"), /name = "nightseam-testee"\nversion = "0.6.0"/);
  assert.match(readFileSync(join(directory, "Cargo.lock"), "utf8"), /name = "tokio"\nversion = "1.47.0"/);
  const testee = join(directory, "conformance/rust/Cargo.toml");
  writeFileSync(testee, readFileSync(testee, "utf8").replace(', version = "0.6.0"', ""));
  assert.deepEqual(rustVersionProblems(directory, "0.6.0"), [], "private testee path dependencies need no publication version");
  const runtime = join(directory, "runtime/rs/Cargo.toml");
  writeFileSync(runtime, readFileSync(runtime, "utf8").replace('version = "0.6.0"', 'version = "0.4.0"'));
  assert.ok(rustVersionProblems(directory, "0.6.0").some(problem => problem.includes("requires nightseam-duplex at 0.4.0")));
});
