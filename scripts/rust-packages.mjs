// Cargo packages follow the repository version without making the fast Go
// tier depend on a Rust toolchain. Read the deliberately simple manifest
// conventions here; the packaged consumer also asks Cargo to resolve them.
import { copyFileSync, existsSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const section = (source, name) => source.match(new RegExp(`^\\[${name.replaceAll(".", "\\.")}\\]\\r?\\n([\\s\\S]*?)(?=^\\[|(?![\\s\\S]))`, "m"))?.[1] ?? "";
const value = (source, key) => source.match(new RegExp(`^${key.replaceAll(".", "\\.")}\\s*=\\s*"([^"]+)"`, "m"))?.[1];
const read = (directory, file) => readFileSync(join(directory, file), "utf8");

/** Public components and private testees, found at the same two-level layout. */
function manifests(directory) {
  const found = [];
  for (const component of readdirSync(directory, { withFileTypes: true })) {
    if (!component.isDirectory() || component.name.startsWith(".") || component.name === "node_modules" || component.name === "target") continue;
    for (const language of readdirSync(join(directory, component.name), { withFileTypes: true })) {
      if (!language.isDirectory()) continue;
      const file = `${component.name}/${language.name}/Cargo.toml`;
      if (!existsSync(join(directory, file))) continue;
      const source = read(directory, file);
      const pkg = section(source, "package");
      const name = value(pkg, "name");
      if (!name) throw new Error(`${file} has no package name`);
      found.push({ file, name, source, public: !/^publish\s*=\s*false\s*$/m.test(pkg) });
    }
  }
  return found.sort((a, b) => a.file.localeCompare(b.file));
}

export const rustPackages = directory => manifests(directory).filter(pkg => pkg.public);

/** Every local dependency must carry the release version when packaged. */
function dependencies(source, names) {
  return [...source.matchAll(/^([\w-]+)\s*=\s*\{([^}\n]*)\}/gm)]
    .filter(match => names.has(match[1]))
    .map(match => ({ name: match[1], version: match[2].match(/\bversion\s*=\s*"([^"]+)"/)?.[1], inherited: /\bworkspace\s*=\s*true\b/.test(match[2]) }));
}

export function rustVersionProblems(directory, version) {
  const crates = manifests(directory);
  if (!crates.length) return [];
  const problems = [];
  const workspace = read(directory, "Cargo.toml");
  const actual = value(section(workspace, "workspace.package"), "version");
  if (actual !== version) problems.push(`Cargo.toml workspace version is ${actual}, the release is ${version}`);
  const names = new Set(crates.map(pkg => pkg.name));
  for (const { file, name, source } of crates) {
    const pkg = section(source, "package");
    const own = /^version\.workspace\s*=\s*true\s*$/m.test(pkg) ? actual : value(pkg, "version");
    if (own !== version) problems.push(`${file} (${name}) is ${own}, the release is ${version}`);
  }
  for (const { file, source, public: published = true } of [{ file: "Cargo.toml", source: workspace }, ...crates]) {
    for (const dep of dependencies(source, names)) {
      if (!dep.inherited && (published || dep.version !== undefined) && dep.version !== version) problems.push(`${file} requires ${dep.name} at ${dep.version ?? "no packaged version"}, the release is ${version}`);
    }
  }
  const lock = read(directory, "Cargo.lock");
  for (const pkg of lock.split(/^\[\[package\]\]\r?\n/m).slice(1)) {
    const name = value(pkg, "name");
    if (names.has(name) && !value(pkg, "source") && value(pkg, "version") !== version) problems.push(`Cargo.lock has ${name} at ${value(pkg, "version")}, the release is ${version}`);
  }
  return problems;
}

/** Move the workspace, explicit local requirements and local lock entries together. */
export function updateRustVersions(directory, version) {
  const crates = manifests(directory);
  if (!crates.length) return;
  const names = new Set(crates.map(pkg => pkg.name));
  const workspace = read(directory, "Cargo.toml");
  const moved = workspace.replace(/(\[workspace\.package\][\s\S]*?^version\s*=\s*)"[^"]+"/m, `$1"${version}"`);
  if (moved === workspace && value(section(workspace, "workspace.package"), "version") !== version) throw new Error("Cargo.toml has no workspace package version");
  for (const { file, source } of [{ file: "Cargo.toml", source: moved }, ...crates]) {
    const next = source.replace(/(\[package\]\r?\n(?:(?!^\[)[\s\S])*?^version\s*=\s*)"[^"]+"/m, `$1"${version}"`)
      .replace(/^([\w-]+)(\s*=\s*\{)([^}\n]*)(\})/gm, (line, name, before, fields, after) => {
        if (!names.has(name)) return line;
        return name + before + fields.replace(/(\bversion\s*=\s*)"[^"]+"/, `$1"${version}"`) + after;
      });
    writeFileSync(join(directory, file), next);
  }
  const lock = read(directory, "Cargo.lock").split(/(^\[\[package\]\]\r?\n)/m).map(pkg => {
    if (!names.has(value(pkg, "name")) || value(pkg, "source")) return pkg;
    return pkg.replace(/^(version\s*=\s*)"[^"]+"/m, `$1"${version}"`);
  }).join("");
  writeFileSync(join(directory, "Cargo.lock"), lock);
  const problems = rustVersionProblems(directory, version);
  if (problems.length) throw new Error(problems.join("\n"));
}

export function copyRustNotices(directory) {
  for (const pkg of rustPackages(directory)) {
    for (const file of ["LICENSE", "NOTICE"]) copyFileSync(join(directory, file), join(directory, pkg.file, "..", file));
  }
}
