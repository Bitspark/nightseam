// Prepares a release without publishing anything: holds every spelling of
// the version to the tag given, copies LICENSE and NOTICE into each package
// so that the tarballs carry them, and writes the tag's changelog section to
// release-notes.md for the GitHub release. The workflow runs it on a tag;
// run it by hand to rehearse. See RELEASING.md.
import { copyFileSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { packages, root } from "./packages.mjs";
const tag = process.argv[2];
if (!/^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/.test(tag ?? "")) {
  console.error("usage: node scripts/release-prepare.mjs v<major.minor.patch>");
  process.exit(2);
}
const version = tag.slice(1);
const problems = [];
for (const directory of packages) {
  const manifest = JSON.parse(readFileSync(join(root, directory, "package.json"), "utf8"));
  if (manifest.version !== version) problems.push(`${directory}/package.json is ${manifest.version}, the tag is ${version}`);
  if (manifest.private) problems.push(`${directory}/package.json is private`);
  copyFileSync(join(root, "LICENSE"), join(root, directory, "LICENSE"));
  copyFileSync(join(root, "NOTICE"), join(root, directory, "NOTICE"));
}
const target = readFileSync(join(root, "internal/targets/typescript/target.go"), "utf8");
const constant = target.match(/DefaultRuntimeVersion = "([^"]+)"/)?.[1];
if (constant !== version) problems.push(`DefaultRuntimeVersion is ${constant}, the tag is ${version}`);
const changelog = readFileSync(join(root, "CHANGELOG.md"), "utf8");
const section = changelog.split(/^## /m).find(part => part.startsWith(version + "\n") || part.startsWith(version + " "));
if (!section) problems.push(`CHANGELOG.md has no section "## ${version}"`);
if (problems.length) {
  console.error("not ready to release:\n  " + problems.join("\n  "));
  process.exit(1);
}
writeFileSync(join(root, "release-notes.md"), section.slice(section.indexOf("\n") + 1).trim() + "\n");
console.log(`ready: ${tag} — ${packages.join(", ")}, DefaultRuntimeVersion, CHANGELOG section; release-notes.md written`);
