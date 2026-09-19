import { execFileSync } from "node:child_process";
import { basename, dirname } from "node:path";

/**
 * Runs `tar` over an archive, naming it from its own directory.
 *
 * GNU tar reads an `-f` operand containing a colon as `host:path` and hands
 * it to `rmt`, so an absolute Windows path — `C:\…\package.tgz` — is taken
 * for a machine called `C` and the command dies with `Cannot connect to C:
 * resolve failed`. That is the tar first on PATH in Git Bash, so the release
 * smokes fail on a Windows checkout and pass on CI's Linux runner: green
 * where they run, red where they are written. The packed smoke is the one
 * gate that asks whether what is published can be *installed*, which makes it
 * the one an author most wants to run before pushing, so it is the last gate
 * that should be unrunnable where the work happens.
 *
 * The colon only matters in `-f`; `-C` is read as a path by every tar. So the
 * archive is named relative to the directory it is in and the command is run
 * there — which needs no branch on the platform and no particular tar, and
 * changes nothing on Linux.
 */
export function tar(directory, args) {
  return execFileSync("tar", args, { cwd: directory, encoding: "utf8" });
}

/** Hold the archive itself to the package contract, before installing it. */
export function holdTarball(file) {
  const entries = new Set(tar(dirname(file), ["-tzf", basename(file)]).trim().split(/\r?\n/));
  const required = ["dist/index.js", "dist/index.d.ts", "README.md", "LICENSE", "NOTICE"];
  const missing = required.filter(name => !entries.has(`package/${name}`));
  if (missing.length) throw new Error(`${file} is missing ${missing.join(", ")}; run pnpm -r build and node scripts/release-prepare.mjs v<version> before packing`);
}
