// Prepares a release without publishing anything: holds every spelling of
// the version to the tag given, holds the conformance matrix to the tier
// table, copies LICENSE and NOTICE into each package so that the tarballs
// carry them, and writes the tag's changelog section to release-notes.md for
// the GitHub release. The workflow runs it on a tag; run it by hand to
// rehearse. See RELEASING.md.
//
//	node scripts/release-prepare.mjs v0.3.0
//	node scripts/release-prepare.mjs v0.3.0 --dry-run    # check and write nothing
//
// --matrix and --previous-matrix name other files, which is how the tests
// drive the gate: the matrices that refuse a release are ones no run has
// produced yet.
import { readFileSync, writeFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { join } from "node:path";
import { gate, matrixFile, missing, profilesFile, readJSON, unrun } from "./matrix.mjs";
import { copyNotices, dependency, examples, manifestsUnder, modules, packages, requirement, root } from "./packages.mjs";
import { rustVersionProblems } from "./rust-packages.mjs";

const flag = name => {
  const at = process.argv.indexOf(name);
  return at < 0 ? undefined : process.argv[at + 1];
};
const dryRun = process.argv.includes("--dry-run");
const tag = process.argv[2];
if (!/^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/.test(tag ?? "")) {
  console.error("usage: node scripts/release-prepare.mjs v<major.minor.patch> [--dry-run]");
  process.exit(2);
}
const version = tag.slice(1);
const problems = [];
problems.push(...rustVersionProblems(root, version));
const pythonProject = readFileSync(join(root, "pyproject.toml"), "utf8").split(/^\[/m).find(section => section.startsWith("project]"));
const pythonVersion = pythonProject?.match(/^version = "([^"]+)"$/m)?.[1];
if (pythonVersion !== version) problems.push(`pyproject.toml is ${pythonVersion ?? "missing its project version"}, the tag is ${version}`);
for (const directory of packages) {
  const manifest = JSON.parse(readFileSync(join(root, directory, "package.json"), "utf8"));
  if (manifest.version !== version) problems.push(`${directory}/package.json is ${manifest.version}, the tag is ${version}`);
  if (manifest.private) problems.push(`${directory}/package.json is private`);
}
const target = readFileSync(join(root, "internal/targets/typescript/target.go"), "utf8");
const constant = target.match(/DefaultRuntimeVersion = "([^"]+)"/)?.[1];
if (constant !== version) problems.push(`DefaultRuntimeVersion is ${constant}, the tag is ${version}`);
// A nested Go module is released by a second tag beside this one and requires
// the root module at the same version, which is one more spelling of it to
// hold to the tag. Nothing is copied into one: a Go module's release is the
// tag, and the licence the repository carries is the licence it is under.
for (const file of modules) {
  const required = readFileSync(join(root, file), "utf8").match(requirement)?.[2];
  if (required !== tag) problems.push(`${file} requires the root module at ${required ?? "nothing"}, the tag is ${tag}`);
}
// The getting-started example is the consumer both smokes install, which
// is only worth anything if what it asks for is what this tag publishes: it
// requires the root module and every published package at the version, with
// no workspace link and no replace to stand in for one.
for (const example of examples) {
  const module = readFileSync(join(root, example, "go.mod"), "utf8");
  const required = module.match(requirement)?.[2];
  if (required !== tag) problems.push(`${example}/go.mod requires the root module at ${required ?? "nothing"}, the tag is ${tag}`);
  if (/^replace\s+github\.com\/Bitspark\/nightseam/m.test(module)) problems.push(`${example}/go.mod replaces the root module; the example resolves what is published`);
  for (const file of manifestsUnder(example)) {
    for (const match of readFileSync(join(root, file), "utf8").matchAll(dependency)) {
      const spec = match[0].slice(match[1].length).slice(1, -1);
      if (spec !== version) problems.push(`${file} depends on ${match[0].split('"')[1]} at ${spec}, the tag is ${version}`);
    }
  }
}

const changelog = readFileSync(join(root, "CHANGELOG.md"), "utf8");
const section = changelog.split(/^## /m).find(part => part.startsWith(version + "\n") || part.startsWith(version + " "));
if (!section) problems.push(`CHANGELOG.md has no section "## ${version}"`);

// The tier table against the matrix `main` recorded. It is the committed
// file and not a run of the suite here: the suite runs later in this same
// job, so a tree that cannot pass fails the tag anyway, and what the gate
// weighs is the standing each language was released at. CI holds the
// committed matrix fresh — a stale one fails the README's table check.
const matrix = readJSON(flag("--matrix") ?? matrixFile);
const profiles = readJSON(profilesFile);
const previous = previousRelease();
// A matrix with a language or a profile missing is the artifact of a run
// filtered by -run, and a tag weighed against one is weighed against what
// nobody ran. A gate reads an absent cell as nothing failing, which is the
// one reading that must never be a release's.
for (const language of missing(matrix, profiles)) {
  problems.push(`conformance/matrix.json has no row for ${language}, which profiles.json places at a tier; it is the artifact of a whole run`);
}
for (const gap of unrun(matrix, profiles)) {
  problems.push(`conformance/matrix.json: ${gap}; a profile with no cell is one this run did not reach, not one that passed`);
}
const { problems: red, provisional, lagging } = gate(matrix, profiles, previous?.matrix);
problems.push(...red);

if (problems.length) {
  console.error("not ready to release:\n  " + problems.join("\n  "));
  process.exit(1);
}

// What the notes must say beyond the changelog: a language the tier table
// ships but marks, and one whose lag this release spends. A consumer reading
// the release learns it here rather than from the matrix they did not open.
const marked = [...provisional, ...lagging];
const notes = section.slice(section.indexOf("\n") + 1).trim() + "\n" + (marked.length ? `\n## Languages\n\n${marked.map(line => `- ${line}.`).join("\n")}\n` : "");

if (dryRun) {
  console.log(`would release ${tag}${marked.length ? `, marking ${marked.length} language${marked.length > 1 ? "s" : ""} in the notes` : ""}; nothing written; LICENSE and NOTICE were not copied`);
} else {
  copyNotices();
  writeFileSync(join(root, "release-notes.md"), notes);
}
const against = previous ? `the matrix of ${previous.tag}` : "no previous release";
console.log(
  `ready: ${tag} — ${packages.join(", ")}, pyproject.toml, DefaultRuntimeVersion, ${modules.join(", ")}, ${examples.join(", ")}, CHANGELOG section` +
    `; the matrix holds against ${against}${marked.length ? `, ${marked.join("; ")}` : ""}` +
    (dryRun ? "" : "; release-notes.md written"),
);

/**
 * The last release's matrix, for the lag a tier 2 language is allowed: the
 * newest `v*` tag that is not this one and carries a matrix. A first release,
 * a shallow checkout with no tags, or a tag cut before the matrix existed all
 * mean the same thing — nothing was failing before, so nothing is a second
 * failure — which is why each is a miss and not an error.
 */
function previousRelease() {
  const named = flag("--previous-matrix");
  if (named) return { tag: named, matrix: readJSON(named) };
  if (process.argv.includes("--no-previous")) return undefined;
  let tags = [];
  try {
    tags = execFileSync("git", ["tag", "--list", "v*", "--sort=-v:refname"], { cwd: root, encoding: "utf8" }).split("\n").filter(Boolean);
  } catch {
    return undefined;
  }
  for (const previous of tags) {
    if (previous === tag) continue;
    try {
      return { tag: previous, matrix: JSON.parse(execFileSync("git", ["show", `${previous}:conformance/matrix.json`], { cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] })) };
    } catch {
      // That release was cut before the matrix was committed; look further back.
    }
  }
  return undefined;
}
