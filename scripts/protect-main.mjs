// The rules on main, applied from .github/ruleset-main.json and held to it.
//
//   node scripts/protect-main.mjs apply    create or update the live ruleset from the file
//   node scripts/protect-main.mjs check    fail if the live ruleset differs from the file
//
// The file is the source of truth (COLLABORATION.md, "How a change lands").
// `check` runs in CI so that a required check removed by a click, or a
// bypass added by hand, fails the next pull request rather than going
// unnoticed; `apply` is run once by someone with admin on the repository,
// and again whenever the file changes. Both go through `gh api`, so the
// caller's own login is the credential and nothing is stored here.
//
// What is compared is what the file governs: enforcement, bypass actors, the
// branch condition, and each rule's type and parameters. GitHub adds fields
// of its own to a ruleset it returns (ids, node ids, links, timestamps) and
// they are ignored.

import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const repo = process.env.GITHUB_REPOSITORY ?? "Bitspark/nightseam";
const file = join(root, ".github", "ruleset-main.json");
const wanted = JSON.parse(readFileSync(file, "utf8"));
delete wanted._comment;

const gh = (args, input) =>
  execFileSync("gh", args, { encoding: "utf8", input, stdio: ["pipe", "pipe", "inherit"] });

/** Every ruleset of the repository, as GitHub lists them. */
function list() {
  return JSON.parse(gh(["api", `repos/${repo}/rulesets`, "--paginate"]));
}

/** One ruleset in full, by id. */
function get(id) {
  return JSON.parse(gh(["api", `repos/${repo}/rulesets/${id}`]));
}

/** The part of a ruleset the file governs, in a canonical shape. */
function governed(r) {
  const rules = [...r.rules]
    .map(x => ({ type: x.type, ...(x.parameters ? { parameters: x.parameters } : {}) }))
    .sort((a, b) => a.type.localeCompare(b.type));
  return {
    name: r.name,
    target: r.target,
    enforcement: r.enforcement,
    bypass_actors: (r.bypass_actors ?? []).map(a => ({ actor_id: a.actor_id, actor_type: a.actor_type, bypass_mode: a.bypass_mode })),
    conditions: r.conditions,
    rules,
  };
}

const live = list().find(r => r.name === wanted.name && r.source_type === "Repository");
const mode = process.argv[2];

if (mode === "apply") {
  const body = JSON.stringify(wanted);
  if (live) {
    gh(["api", "--method", "PUT", `repos/${repo}/rulesets/${live.id}`, "--input", "-"], body);
    console.log(`updated ruleset ${wanted.name} (${live.id}) from ${file}`);
  } else {
    const made = JSON.parse(gh(["api", "--method", "POST", `repos/${repo}/rulesets`, "--input", "-"], body));
    console.log(`created ruleset ${wanted.name} (${made.id}) from ${file}`);
  }
} else if (mode === "check") {
  if (!live) {
    console.error(`no live ruleset named ${wanted.name} on ${repo}; run: node scripts/protect-main.mjs apply`);
    process.exit(1);
  }
  const a = JSON.stringify(governed(get(live.id)));
  const b = JSON.stringify(governed(wanted));
  if (a !== b) {
    console.error(`the live ruleset ${wanted.name} has drifted from ${file}:\n  live: ${a}\n  file: ${b}\nrun: node scripts/protect-main.mjs apply — or change the file, which is the source of truth`);
    process.exit(1);
  }
  console.log(`ruleset ${wanted.name} matches ${file}`);
} else {
  console.error("usage: node scripts/protect-main.mjs apply|check");
  process.exit(2);
}
