// Sets the one version everywhere it is spelled: every package manifest,
// the generator's DefaultRuntimeVersion, the version every nested Go module
// requires the root module at, and what the getting-started example under
// examples/ — a consumer checkout, which resolves through no link and no
// replace — depends on in both languages. The Go modules' versions are
// their tags, cut afterwards; the goldens that embed the constant are
// rewritten by `go test ./cmd/nightseam -short -run Golden -update`. See
// RELEASING.md.
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { dependency, examples, manifestsUnder, modules, packages, requirement, root } from "./packages.mjs";
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
// Every nested Go module requires the root module at the release's number;
// scripts/release-prepare.mjs holds it to the tag, as it holds the manifests,
// and TestVersionsMoveInLockstep holds it in the fast tier.
for (const file of modules) {
  const path = join(root, file);
  const module = readFileSync(path, "utf8");
  if (!requirement.test(module)) {
    console.error("no requirement on the root module in " + path);
    process.exit(1);
  }
  writeFileSync(path, module.replace(requirement, `$1github.com/Bitspark/nightseam v${version}`));
}
// The getting-started example is a consumer checkout and not a workspace
// member: it names the released version of every package it depends on,
// with no `workspace:*` and no `replace`, which is what lets the two smokes
// install it from outside. Those spellings move here with the rest, and
// they are rewritten in place rather than through JSON so that the manifest
// the generator renders keeps the formatting `nightseam check` holds it to.
for (const example of examples) {
  const modfile = join(root, example, "go.mod");
  const module = readFileSync(modfile, "utf8");
  if (!requirement.test(module)) {
    console.error("no requirement on the root module in " + modfile);
    process.exit(1);
  }
  writeFileSync(modfile, module.replace(requirement, `$1github.com/Bitspark/nightseam v${version}`));
  for (const file of manifestsUnder(example)) {
    const path = join(root, file);
    const manifest = readFileSync(path, "utf8");
    const moved = manifest.replace(dependency, `$1"${version}"`);
    if (moved !== manifest) writeFileSync(path, moved);
  }
}
console.log(`version ${version}: ${packages.join(", ")}, DefaultRuntimeVersion, ${modules.join(", ")}${examples.length ? ", " + examples.join(", ") : ""}`);
