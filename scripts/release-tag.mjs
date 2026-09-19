// Check the remote tag against this checkout before releasing its contents.
// A rehearsal may use an unpublished name; a tag-triggered run requires it.
// This only reads refs and never creates, moves or deletes a tag.
//
//   node scripts/release-tag.mjs v0.4.0 [--require]
import { execFileSync } from "node:child_process";

const [tag, flag, ...extra] = process.argv.slice(2);
if (!/^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/.test(tag ?? "") || (flag && flag !== "--require") || extra.length) {
  console.error("usage: node scripts/release-tag.mjs v<major.minor.patch> [--require]");
  process.exit(2);
}
const git = (...args) => execFileSync("git", args, { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
try {
  const commit = git("rev-parse", "HEAD^{commit}");
  const ref = `refs/tags/${tag}`;
  const refs = new Map(git("ls-remote", "origin", ref, `${ref}^{}`).split("\n").filter(Boolean).map(line => {
    const [sha, name] = line.split(/\s+/);
    return [name, sha];
  }));
  // An annotated tag's object has a different id from the commit it names.
  const published = refs.get(`${ref}^{}`) ?? refs.get(ref);
  if (!published) {
    if (flag === "--require") throw new Error(`${tag} is missing from origin; a release requires its published tag`);
    console.log(`${tag} has not been published; this checkout may be rehearsed`);
  } else if (published !== commit) {
    throw new Error(`${tag} already names ${published} on origin, not the checked-out commit ${commit}; release tags are immutable`);
  } else {
    console.log(`${tag} matches the checked-out commit ${commit}`);
  }
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}
