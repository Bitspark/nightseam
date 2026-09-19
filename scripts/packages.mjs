// What a release moves, found rather than listed: the published packages, and
// the Go modules nested beside them. pnpm-workspace.yaml makes every <name>/ts
// a workspace package, and a <name>/go that carries a go.mod is a module of
// its own; this reads both shapes off the disk, so that a component added
// beside the others is versioned and released with them without anyone
// remembering to name it in three places. The Go side of the same rule is
// TestVersionsMoveInLockstep, which globs the same paths.
import { copyFileSync, cpSync, existsSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

export const root = join(dirname(fileURLToPath(import.meta.url)), "..");

const components = readdirSync(root, { withFileTypes: true })
  .filter(entry => entry.isDirectory() && entry.name !== "node_modules")
  .map(entry => entry.name)
  .sort();

/** Every <name>/ts that carries a manifest and is not private, in path order. */
export const packages = components
  .map(name => `${name}/ts`)
  .filter(directory => existsSync(join(root, directory, "package.json")))
  .filter(directory => !JSON.parse(readFileSync(join(root, directory, "package.json"), "utf8")).private);

/** Lay down the repository notices in every package before packing or publishing. */
export function copyNotices() {
  for (const directory of packages) {
    for (const name of ["LICENSE", "NOTICE"]) copyFileSync(join(root, name), join(root, directory, name));
  }
}

/** Copy the registry's consumer with its own workspace and unchanged release manifests. */
export function copyRegistryConsumer(source, destination) {
  cpSync(source, destination, { recursive: true, filter: path => !/[\\/](node_modules|dist)$/.test(path) });
  // Temp directories may live beneath a user's workspace. A boundary in
  // the copy stops pnpm from installing or changing that ancestor project.
  const workspace = join(destination, "pnpm-workspace.yaml");
  if (!existsSync(workspace)) writeFileSync(workspace, "packages: []\n");
}

/**
 * Every <name>/go that is a module of its own: a component depending on what
 * the core module may not, released by a second tag `<dir>/vX.Y.Z` cut beside
 * `vX.Y.Z` and requiring the root module at that same version. otel/go, the
 * OpenTelemetry adapter, is the first. In path order.
 */
export const modules = components
  .map(name => `${name}/go/go.mod`)
  .filter(file => existsSync(join(root, file)));

/**
 * Every directory under examples/ that is a consumer checkout of its own:
 * the getting-started, which carries no `workspace:*` and no `replace` and
 * therefore names the released version of every Nightseam package it
 * depends on. That is what makes it the consumer the two smokes install —
 * from the packed tarballs before the tag, from the registries after it —
 * and it is one more place the version is spelled, so scripts/version.mjs
 * moves it with the rest and TestVersionsMoveInLockstep holds it there.
 * In path order.
 */
export const examples = existsSync(join(root, "examples"))
  ? readdirSync(join(root, "examples"), { withFileTypes: true })
      .filter(entry => entry.isDirectory())
      .map(entry => `examples/${entry.name}`)
      .sort()
  : [];

/** Every package.json below `directory`, in path order; an example has one of its own and one the generator renders. */
export function manifestsUnder(directory) {
  const found = [];
  const walk = at => {
    for (const entry of readdirSync(join(root, at), { withFileTypes: true }).sort((a, b) => (a.name < b.name ? -1 : 1))) {
      if (entry.isDirectory()) {
        if (entry.name !== "node_modules" && entry.name !== "dist") walk(`${at}/${entry.name}`);
      } else if (entry.name === "package.json") {
        found.push(`${at}/package.json`);
      }
    }
  };
  walk(directory);
  return found;
}

/**
 * How a module requires the root module: what stands before the path — an
 * indent inside a require block, or the `require` keyword itself, as the
 * example spells it — and the version that moves with the release. The
 * `replace` beside a nested module's requirement names no version and is
 * left alone: it is this checkout's, and a consumer ignores it.
 */
export const requirement = /^(\s*(?:require\s+)?)github\.com\/Bitspark\/nightseam (v\S+)$/m;

/** How a manifest spells its dependency on a published package, whatever the manifest's own formatting. */
export const dependency = /("@nightseam\/[a-z0-9-]+"\s*:\s*)"[^"]*"/g;

if (packages.length === 0) {
  console.error("no published package found under */ts");
  process.exit(1);
}
