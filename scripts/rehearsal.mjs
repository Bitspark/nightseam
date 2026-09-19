import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { layProxy } from "./modzip.mjs";
import { requirement } from "./packages.mjs";

/** Prepare the Go half of the packed smoke, scoped to its copied consumer. */
export function prepareGoRehearsal({ root, scratch, consumer, module, version }) {
  const revision = execFileSync("git", ["rev-parse", "HEAD"], { cwd: root, encoding: "utf8" }).trim();
  const rehearsalVersion = `v${version}${version.includes("-") ? "." : "-"}rehearsal.${revision}`;
  const manifest = join(consumer, "go.mod");
  const source = readFileSync(manifest, "utf8");
  if (!requirement.test(source)) throw new Error(`${manifest} does not require ${module}`);
  writeFileSync(manifest, source.replace(requirement, (_, prefix) => `${prefix}${module} ${rehearsalVersion}`));
  const proxy = layProxy({ root, directory: join(scratch, "goproxy"), module, version: rehearsalVersion });
  return {
    version: rehearsalVersion,
    env: {
      GOWORK: "off",
      // Rehearsal bits must never enter a consumer's shared module cache.
      // Writable cache entries let the smoke remove its scratch directory.
      GOMODCACHE: join(scratch, "gomodcache"),
      GOFLAGS: "-mod=mod -modcacherw",
      GOSUMDB: "off",
      GOPRIVATE: "none",
      GONOPROXY: "none",
      GOPROXY: [pathToFileURL(proxy).href, process.env.GOPROXY || "https://proxy.golang.org,direct"].join(","),
    },
  };
}
