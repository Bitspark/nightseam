import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const script = fileURLToPath(new URL("./worktree.mjs", import.meta.url));

function fixture(t) {
  const directory = mkdtempSync(join(tmpdir(), "nightseam worktree test-"));
  t.after(() => rmSync(directory, { recursive: true, force: true, maxRetries: 10 }));
  const repo = join(directory, "repo");
  mkdirSync(join(repo, "scripts"), { recursive: true });
  copyFileSync(script, join(repo, "scripts/worktree.mjs"));
  writeFileSync(join(repo, ".gitignore"), ".worktrees/\n");
  const git = (...args) => execFileSync("git", ["-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", ...args], { cwd: repo, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
  git("init", "--quiet");
  git("add", ".");
  git("commit", "--quiet", "-m", "fixture: create a worktree repository.");
  const remove = branch => {
    const result = spawnSync(process.execPath, [join(repo, "scripts/worktree.mjs"), "rm", branch], { cwd: directory, encoding: "utf8" });
    return { code: result.status, out: result.stdout + result.stderr };
  };
  return { directory, repo, git, remove };
}

test("rm removes a registered worktree and its branch, and can be repeated", t => {
  const { repo, git, remove } = fixture(t);
  const branch = "issue-999-registered";
  const path = join(repo, ".worktrees", branch);
  git("worktree", "add", "--quiet", "-b", branch, path);
  const result = remove(branch);
  assert.equal(result.code, 0, result.out);
  assert.equal(existsSync(path), false);
  assert.equal(git("branch", "--list", branch), "");
  const repeated = remove(branch);
  assert.equal(repeated.code, 0, repeated.out);
});

test("rm recognizes a registered worktree through a differently spelled path, such as a Windows short name", t => {
  // The GitHub Windows runner's temp is spelled C:\Users\RUNNER~1\…; git
  // lists the worktree by its long name, and a script that compared
  // spellings took a registered worktree for a leftover and failed on its
  // contents (#624).
  const { directory, repo, git } = fixture(t);
  const branch = "issue-999-short";
  const path = join(repo, ".worktrees", branch);
  git("worktree", "add", "--quiet", "-b", branch, path);
  // Another spelling of the same directory: on Windows, where the filesystem
  // folds case as it folds 8.3 names to long ones, the path in upper case;
  // elsewhere a symbolic link beside it. Either names the repository the
  // script is copied into, and git names it its own way.
  let spelled;
  if (process.platform === "win32") spelled = repo.toUpperCase();
  else {
    spelled = join(directory, "spelled");
    symlinkSync(repo, spelled);
  }
  const result = spawnSync(process.execPath, [join(spelled, "scripts/worktree.mjs"), "rm", branch], { cwd: repo, encoding: "utf8" });
  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.match(result.stdout, /removed worktree/);
  assert.equal(existsSync(path), false);
  assert.equal(git("branch", "--list", branch), "");
});

test("rm clears an unregistered leftover without confusing a longer worktree path", t => {
  const { repo, git, remove } = fixture(t);
  const branch = "issue-999-leftover";
  const path = join(repo, ".worktrees", branch);
  const other = path + "-other";
  git("worktree", "add", "--quiet", "-b", branch + "-other", other);
  git("branch", branch);
  mkdirSync(path);
  const result = remove(branch);
  assert.equal(result.code, 0, result.out);
  assert.match(result.out, /leftover/);
  assert.equal(existsSync(path), false);
  assert.equal(git("branch", "--list", branch), "");
  assert.equal(existsSync(join(other, ".git")), true);
  assert.notEqual(git("branch", "--list", branch + "-other"), "");
});

test("rm prunes a missing worktree before deleting its branch", t => {
  const { repo, git, remove } = fixture(t);
  const branch = "issue-999-missing";
  const path = join(repo, ".worktrees", branch);
  git("worktree", "add", "--quiet", "-b", branch, path);
  rmSync(path, { recursive: true, force: true });
  const result = remove(branch);
  assert.equal(result.code, 0, result.out);
  assert.equal(git("branch", "--list", branch), "");
  assert.equal(git("worktree", "list", "--porcelain").includes(branch), false);
});

test("rm preserves files in an unregistered directory and leaves its branch", t => {
  const { repo, git, remove } = fixture(t);
  const branch = "issue-999-unregistered-work";
  const path = join(repo, ".worktrees", branch);
  git("branch", branch);
  mkdirSync(path, { recursive: true });
  writeFileSync(join(path, "keep.txt"), "keep\n");
  const result = remove(branch);
  assert.equal(result.code, 1, result.out);
  assert.match(result.out, /cannot remove leftover directory/);
  assert.equal(readFileSync(join(path, "keep.txt"), "utf8"), "keep\n");
  assert.notEqual(git("branch", "--list", branch), "");
});

test("rm refuses a path outside its worktree directory", t => {
  const { repo, remove } = fixture(t);
  const marker = join(repo, "keep.txt");
  writeFileSync(marker, "keep\n");
  const result = remove("..");
  assert.equal(result.code, 2, result.out);
  assert.match(result.out, /usage:/);
  assert.equal(readFileSync(marker, "utf8"), "keep\n");
});
