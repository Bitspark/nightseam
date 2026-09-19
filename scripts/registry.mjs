import { readFileSync } from "node:fs";
import { join } from "node:path";
import { setTimeout as after } from "node:timers/promises";
import { modules, packages, root } from "./packages.mjs";

/**
 * Publishing can finish before either registry serves the new version.
 * A Go proxy miss can remain cached for many minutes after the tag exists.
 * Hold every package and module to one deadline before the smoke installs
 * anything; endpoints and the budget can be replaced by a local test registry.
 */
export async function waitForRegistries(tag, {
  npmRegistry = "https://registry.npmjs.org",
  goProxy = "https://proxy.golang.org",
  timeoutMs = 30 * 60_000,
  log = console.log,
  clock = { now: () => performance.now(), sleep: after },
} = {}) {
  const start = clock.now();
  const deadline = start + timeoutMs;
  const elapsed = () => ((clock.now() - start) / 1000).toFixed(1);
  const version = tag.slice(1);
  const targets = packages.map(directory => {
    const { name } = JSON.parse(readFileSync(join(root, directory, "package.json"), "utf8"));
    return { name: `${name}@${version}`, url: `${npmRegistry}/${encodeURIComponent(name)}`, field: data => data?.["dist-tags"]?.latest, version };
  });
  for (const file of ["go.mod", ...modules]) {
    const name = readFileSync(join(root, file), "utf8").match(/^module (\S+)/m)[1];
    // Go proxy paths escape uppercase letters in both module and version.
    const escape = value => value.replace(/[A-Z]/g, letter => `!${letter.toLowerCase()}`);
    targets.push({ name: `${name}@${tag}`, url: `${goProxy}/${escape(name)}/@v/${escape(tag)}.info`, field: data => data?.Version, version: tag });
  }
  const pending = new Set(targets);
  let delay = 1000;
  log(`waiting up to ${timeoutMs / 1000}s for ${targets.length} packages and modules to propagate`);
  while (pending.size && clock.now() < deadline) {
    await Promise.all([...pending].map(async target => {
      const remaining = deadline - clock.now();
      if (remaining <= 0) return;
      try {
        // The timeout covers headers and the response body. A slow registry
        // gets another attempt without spending the whole propagation window.
        const signal = AbortSignal.timeout(Math.ceil(Math.min(10_000, remaining)));
        const response = await fetch(target.url, { signal });
        if (!response.ok) {
          target.last = `HTTP ${response.status}`;
          await response.body?.cancel();
          return;
        }
        const actual = target.field(await response.json());
        if (actual === target.version) {
          pending.delete(target);
          log(`${target.name} available after ${elapsed()}s`);
        } else {
          target.last = `version ${JSON.stringify(actual) ?? "missing"}, expected ${target.version}`;
        }
      } catch (error) {
        target.last = error.message;
      }
    }));
    if (!pending.size) return;
    const remaining = deadline - clock.now();
    if (remaining <= 0) break;
    const pause = Math.min(delay, remaining);
    log(`${pending.size} still unavailable after ${elapsed()}s; retrying in ${(pause / 1000).toFixed(1)}s`);
    // Timers can wake before a fractional delay has elapsed. Hold the
    // scheduled wakeup, especially the final deadline, before retrying.
    const wakeAt = Math.min(clock.now() + pause, deadline);
    while (clock.now() < wakeAt) await clock.sleep(Math.ceil(wakeAt - clock.now()));
    delay = Math.min(delay * 2, 30_000);
  }
  throw new Error(`registry propagation timed out after ${elapsed()}s:\n${[...pending].map(target => `  ${target.name}: ${target.last ?? "no response"}`).join("\n")}`);
}
