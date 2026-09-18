// Sets the one version everywhere it is spelled: every package manifest, the
// generator's DefaultRuntimeVersion, and the version every nested Go module
// requires the root module at. The Go modules' versions are their tags, cut
// afterwards; the goldens that embed the constant are rewritten by
// `go test ./cmd/nightseam -short -run Golden -update`. See RELEASING.md.
import { existsSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { packages, root } from "./packages.mjs";
const version = process.argv[2];
if (!/^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/.test(version ?? "")) {
  console.error("usage: node scripts/version.mjs <major.minor.patch>");
  process.exit(2);
}
for (const directory of packages) {
  const file = join(root, directory, "package.json");
  const manifest = JSON.parse(readFileSync(file, "utf8"));
  manifest.version = version;
  writeFileSync(file, JSON.stringify(manifest, null, 2) + "\n");
}
const target = join(root, "internal/targets/typescript/target.go");
const source = readFileSync(target, "utf8");
const constant = /DefaultRuntimeVersion = "[^"]+"/;
if (!constant.test(source)) {
  console.error("DefaultRuntimeVersion not found in " + target);
  process.exit(1);
}
writeFileSync(target, source.replace(constant, `DefaultRuntimeVersion = "${version}"`));
// A component that may depend on what the core module may not — the
// OpenTelemetry adapter is the first — is a Go module of its own, released by
// a second tag beside the root module's and requiring the root module at that
// same number. They are found rather than listed, as the packages are, and
// TestVersionsMoveInLockstep globs the same paths. The `replace` beside the
// requirement is this checkout's and stays as it is: a consumer ignores a
// dependency's replace and gets what is required.
const requirement = /^(\s*)github\.com\/Bitspark\/nightseam v\S+$/m;
const modules = readdirSync(root, { withFileTypes: true })
  .filter(entry => entry.isDirectory() && entry.name !== "node_modules")
  .map(entry => `${entry.name}/go/go.mod`)
  .filter(file => existsSync(join(root, file)))
  .sort();
for (const file of modules) {
  const path = join(root, file);
  const module = readFileSync(path, "utf8");
  if (!requirement.test(module)) {
    console.error("no requirement on the root module in " + path);
    process.exit(1);
  }
  writeFileSync(path, module.replace(requirement, `$1github.com/Bitspark/nightseam v${version}`));
}
console.log(`version ${version}: ${packages.join(", ")}, DefaultRuntimeVersion, ${modules.join(", ")}`);
