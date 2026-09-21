// Build and use only what cargo package puts into its public .crate files.
// No registry publication: the outside consumer patches exact release
// requirements to the extracted archives, never to repository source.
import { execFileSync } from "node:child_process";
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, dirname, isAbsolute, join, relative } from "node:path";
import { root } from "./packages.mjs";
import { copyRustNotices, rustPackages, rustVersionProblems } from "./rust-packages.mjs";
import { tar } from "./tarball.mjs";

const keep = process.argv.includes("--keep");
const scratch = mkdtempSync(join(tmpdir(), "nightseam-rust-packed-"));
const version = JSON.parse(readFileSync(join(root, "runtime/ts/package.json"), "utf8")).version;
const crates = rustPackages(root);
const cargo = (args, cwd = root, capture = false) => execFileSync("cargo", args, {
  cwd,
  encoding: "utf8",
  stdio: capture ? ["ignore", "pipe", "inherit"] : "inherit",
  env: { ...process.env, CARGO_TARGET_DIR: join(scratch, "target") },
});

try {
  // An OS temp override inside the checkout must not weaken the outsider test.
  const inside = relative(realpathSync(root), realpathSync(scratch));
  if (!isAbsolute(inside) && inside !== ".." && !inside.startsWith("..\\") && !inside.startsWith("../")) throw new Error("the Rust smoke temporary directory must be outside the repository");
  const problems = rustVersionProblems(root, version);
  if (problems.length) throw new Error(problems.join("\n"));
  if (!crates.length) throw new Error("no public Rust crates found");
  copyRustNotices(root);
  // The consumer below does the actual verification with both public crates.
  // Selecting all public crates together lets Cargo stage their dependencies
  // while constructing normalized manifests and package lockfiles.
  cargo(["package", "--locked", "--allow-dirty", "--no-verify", ...crates.flatMap(pkg => ["--package", pkg.name])]);
  const packed = join(scratch, "crates");
  mkdirSync(packed);
  for (const pkg of crates) {
    const archive = join(scratch, "target", "package", `${pkg.name}-${version}.crate`);
    const prefix = `${pkg.name}-${version}/`;
    const entries = tar(dirname(archive), ["-tzf", basename(archive)]).trim().split(/\r?\n/);
    for (const file of ["Cargo.toml", "src/lib.rs", "README.md", "LICENSE", "NOTICE"]) {
      if (!entries.includes(prefix + file)) throw new Error(`${archive} omits ${file}`);
    }
    tar(dirname(archive), ["-xzf", basename(archive), "-C", packed]);
    const manifest = readFileSync(join(packed, prefix, "Cargo.toml"), "utf8");
    if (/^\[workspace(?:\.|\])/m.test(manifest) || manifest.split(/(?=^\[)/m).some(section => /^\[[^\n]*dependencies\./.test(section) && /^\s*path\s*=/m.test(section))) throw new Error(`${archive} still needs a repository path or workspace`);
  }
  const consumer = join(scratch, "consumer");
  mkdirSync(join(consumer, "src"), { recursive: true });
  copyFileSync(join(root, "scripts/rust-consumer.rs"), join(consumer, "src/main.rs"));
  writeFileSync(join(consumer, "Cargo.toml"), [
    '[package]', 'name = "nightseam-packaged-consumer"', 'version = "0.0.0"', 'edition = "2024"', 'publish = false',
    '[workspace]', '[dependencies]',
    ...crates.map(pkg => `${pkg.name} = "=${version}"`),
    'tokio = { version = "1", features = ["rt-multi-thread", "macros", "time"] }', 'serde_json = "1"',
    '[patch.crates-io]',
    ...crates.map(pkg => `${pkg.name} = { path = "../crates/${pkg.name}-${version}" }`), '',
  ].join("\n"));
  // Resolve the whole graph and prove no Nightseam package came from the
  // working tree or a registry version accidentally satisfying the request.
  const metadata = JSON.parse(cargo(["metadata", "--format-version", "1"], consumer, true));
  for (const pkg of crates) {
    const found = metadata.packages.find(entry => entry.name === pkg.name && entry.version === version);
    if (!found || realpathSync(found.manifest_path) !== realpathSync(join(packed, `${pkg.name}-${version}`, "Cargo.toml"))) throw new Error(`${pkg.name} did not resolve to its packed archive`);
  }
  cargo(["run", "--locked", "--quiet"], consumer);
} finally {
  if (keep) console.log(`Rust packed consumer kept at ${scratch}`);
  else rmSync(scratch, { recursive: true, force: true });
}
