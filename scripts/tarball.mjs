import { execFileSync } from "node:child_process";

/** Hold the archive itself to the package contract, before installing it. */
export function holdTarball(file) {
  const entries = new Set(execFileSync("tar", ["-tzf", file], { encoding: "utf8" }).trim().split(/\r?\n/));
  const required = ["dist/index.js", "dist/index.d.ts", "README.md", "LICENSE", "NOTICE"];
  const missing = required.filter(name => !entries.has(`package/${name}`));
  if (missing.length) throw new Error(`${file} is missing ${missing.join(", ")}; run pnpm -r build and node scripts/release-prepare.mjs v<version> before packing`);
}
