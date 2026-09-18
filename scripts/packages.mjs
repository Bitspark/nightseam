// What a release moves, found rather than listed: the published packages, and
// the Go modules nested beside them. pnpm-workspace.yaml makes every <name>/ts
// a workspace package, and a <name>/go that carries a go.mod is a module of
// its own; this reads both shapes off the disk, so that a component added
// beside the others is versioned and released with them without anyone
// remembering to name it in three places. The Go side of the same rule is
// TestVersionsMoveInLockstep, which globs the same paths.
import { existsSync, readdirSync, readFileSync } from "node:fs";
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
 * How a nested module requires the root module: the indent, and the version
 * that moves with the release. The `replace` beside it names no version and
 * is left alone — it is this checkout's, and a consumer ignores it.
 */
export const requirement = /^(\s*)github\.com\/Bitspark\/nightseam (v\S+)$/m;

if (packages.length === 0) {
  console.error("no published package found under */ts");
  process.exit(1);
}
