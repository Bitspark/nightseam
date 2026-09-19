// A lane's own checkout: a git worktree on a branch of its own, so that
// lanes running at once never share a working tree or an index.
//
//   node scripts/worktree.mjs add <branch>    fetch origin/main, create .worktrees/<branch> on a new branch from it
//   node scripts/worktree.mjs rm <branch>     remove the worktree and its local branch (the remote one goes with the PR)
//   node scripts/worktree.mjs ls              what exists
//
// Branch names are `issue-<N>-<slug>` for a lane, `docs-<slug>` or
// `release-<version>` otherwise. .worktrees/ is gitignored, so a worktree
// never appears as an untracked file in another. COLLABORATION.md, "How a
// change lands", is the whole lifecycle; this is its first step.

import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const dir = join(root, ".worktrees");
const git = (...args) => execFileSync("git", args, { cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "inherit"] });

const [mode, branch] = process.argv.slice(2);
const valid = /^[a-z0-9][a-z0-9._-]*$/;

if (mode === "add") {
  if (!branch || !valid.test(branch)) {
    console.error("usage: node scripts/worktree.mjs add <branch>   (lower case, digits, . _ -)");
    process.exit(2);
  }
  const path = join(dir, branch);
  if (existsSync(path)) {
    // A directory git does not know is a leftover — an empty dir a shell held
    // open while rm ran — and is cleared; one git knows is a worktree in use.
    const known = git("worktree", "list", "--porcelain").includes(path.replaceAll("\\", "/"));
    if (known) { console.error(`${path} is a worktree in use; rm it first, or choose another branch`); process.exit(1); }
    rmSync(path, { recursive: true, force: true });
    git("worktree", "prune");
  }
  mkdirSync(dir, { recursive: true });
  git("fetch", "--quiet", "origin", "main");
  git("worktree", "add", "--quiet", "-b", branch, path, "origin/main");
  console.log(`${path}\non branch ${branch} from origin/main — cd there, work, then: git push -u origin ${branch} && gh pr create --fill && gh pr merge --auto --squash --delete-branch`);
} else if (mode === "rm") {
  if (!branch) { console.error("usage: node scripts/worktree.mjs rm <branch>"); process.exit(2); }
  const path = join(dir, branch);
  const had = existsSync(path);
  if (had) git("worktree", "remove", "--force", path);
  git("worktree", "prune");
  let branchGone = false;
  try { git("branch", "-D", branch); branchGone = true; } catch { /* already gone with the PR */ }
  console.log(`${had ? "removed" : "no worktree at"} .worktrees/${branch}; local branch ${branchGone ? "deleted" : "already gone"}`);
} else if (mode === "ls") {
  process.stdout.write(git("worktree", "list"));
} else {
  console.error("usage: node scripts/worktree.mjs add|rm <branch> | ls");
  process.exit(2);
}
