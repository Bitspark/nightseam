// Sets the one version everywhere it is spelled: every package manifest
// and the generator's DefaultRuntimeVersion. The Go module's version is its
// tag, cut afterwards; the goldens that embed the constant are rewritten by
// `go test ./cmd/nightseam -short -run Golden -update`. See RELEASING.md.
import { readFileSync, writeFileSync } from "node:fs";
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
console.log(`version ${version}: ${packages.join(", ")}, DefaultRuntimeVersion`);
