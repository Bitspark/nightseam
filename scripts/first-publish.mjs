// Which of the names this release publishes npm has never served, asked
// before anything is uploaded.
//
//	node scripts/first-publish.mjs                       # report
//	node scripts/first-publish.mjs --require-credential  # and refuse one that cannot be made
//
// npm configures trusted publishing on a package that already exists, so the
// first publish of a name cannot authenticate by OIDC: it needs a granular
// token on the scope, and the trusted publisher is attached to the name
// afterwards. RELEASING.md's *A name npm has not served* says the whole of
// it. The cost of finding out late is why this runs early — a name found
// under `*/ts` is published by `pnpm -r publish`, which reaches it after the
// tag is pushed and after the names before it are already on npm, and
// neither of those can be taken back.
//
// It reads the public registry and sends no credential: whether a name exists
// is public, and this asks nothing else. A name it cannot get an answer
// about is a refusal rather than a new name, since "not found" and "could not
// ask" differ by exactly the thing being decided.
//
// Nothing here calls process.exit: a fetch leaves the agent's sockets open,
// and exiting out from under them aborts the process on Windows rather than
// returning the code that was asked for. The exit code is set and the script
// returns, which is also what lets the last line be written in full.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { packages, root } from "./packages.mjs";

const args = process.argv.slice(2);
const flag = name => {
  const at = args.indexOf(name);
  return at < 0 ? undefined : args[at + 1];
};
const known = ["--registry", "--require-credential"];

if (args.some(argument => argument.startsWith("--") && !known.includes(argument))) {
  console.error("usage: node scripts/first-publish.mjs [--registry <url>] [--require-credential]");
  process.exitCode = 2;
} else {
  await report();
}

async function report() {
  const registry = (flag("--registry") ?? process.env.NPM_CONFIG_REGISTRY ?? "https://registry.npmjs.org").replace(/\/+$/, "");
  const names = packages.map(directory => ({
    directory,
    name: JSON.parse(readFileSync(join(root, directory, "package.json"), "utf8")).name,
  }));

  const fresh = [];
  for (const { directory, name } of names) {
    // The abbreviated document: the registry answers it for every package, it
    // is a fraction of the full one, and all that is read from it is that
    // there was one. The connection is closed after each so that nothing is
    // left holding the process open.
    const response = await fetch(`${registry}/${name.replace("/", "%2f")}`, {
      headers: { accept: "application/vnd.npm.install-v1+json", connection: "close" },
    });
    await response.arrayBuffer();
    if (response.status === 404) fresh.push({ directory, name });
    else if (!response.ok) {
      console.error(`${name}: the registry answered ${response.status} ${response.statusText}; a name nobody could ask about is not a name nobody has published`);
      process.exitCode = 1;
      return;
    }
  }

  const served = names.length - fresh.length;
  if (fresh.length === 0) {
    console.log(`${served} published name${served === 1 ? "" : "s"}, every one of them already on ${registry}; each publishes by its trusted publisher`);
    return;
  }

  for (const { directory, name } of fresh) console.log(`first publish: ${name} (${directory}) — ${registry} has never served it`);
  const credential = (process.env.NODE_AUTH_TOKEN || process.env.NPM_TOKEN || "").trim();
  if (args.includes("--require-credential") && !credential) {
    console.error(
      `not ready to publish: ${fresh.map(({ name }) => name).join(", ")} ${fresh.length === 1 ? "is a name" : "are names"} the registry has never served, and no NODE_AUTH_TOKEN is set.\n` +
        "  npm attaches a trusted publisher to a package that exists, so a first publish authenticates by token and not by OIDC.\n" +
        "  RELEASING.md, *A name npm has not served*, says what to provision and what to take away again afterwards.",
    );
    process.exitCode = 1;
    return;
  }
  console.log(
    `${fresh.length} first publish${fresh.length === 1 ? "" : "es"} beside ${served} established name${served === 1 ? "" : "s"}` +
      (credential ? "; a credential is set, so the upload can make them" : "; no credential is set, which a rehearsal does not need"),
  );
}
