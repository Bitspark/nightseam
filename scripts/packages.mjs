// The published packages, found rather than listed. pnpm-workspace.yaml makes
// every <name>/ts a workspace package; this reads the same shape off the disk,
// so that a component added beside the others is versioned and released with
// them without anyone remembering to name it in three places. The Go side of
// the same rule is TestVersionsMoveInLockstep, which globs the same paths.
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

export const root = join(dirname(fileURLToPath(import.meta.url)), "..");

/** Every <name>/ts that carries a manifest and is not private, in path order. */
export const packages = readdirSync(root, { withFileTypes: true })
  .filter(entry => entry.isDirectory() && entry.name !== "node_modules")
  .map(entry => `${entry.name}/ts`)
  .filter(directory => existsSync(join(root, directory, "package.json")))
  .filter(directory => !JSON.parse(readFileSync(join(root, directory, "package.json"), "utf8")).private)
  .sort();

if (packages.length === 0) {
  console.error("no published package found under */ts");
  process.exit(1);
}
