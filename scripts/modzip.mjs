// A Go module zip, written from this checkout without a network and without
// a tag. The module proxy protocol is four files per version — list, .info,
// .mod and .zip — and the zip is the whole of what `go` unpacks into its
// module cache: every path inside it is `<module>@<version>/…`, and the
// toolchain hashes what it finds there rather than trusting where it came
// from, so a zip laid here is indistinguishable to a consumer from one the
// proxy would serve for the tag.
//
// What goes in is what git tracks (plus what it would track), minus every
// subtree that carries a go.mod of its own: a nested module is released by
// its own tag and is never part of its parent's zip, which is the rule the
// toolchain applies when it builds one from a tag, and which keeps the
// example — a module of its own — out of the module it consumes.
import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { join, posix } from "node:path";
import { crc32, deflateRawSync } from "node:zlib";

/** The proxy's escaping of a module path: an upper-case letter is `!` and its lower case. */
export const escapeModule = module => module.replace(/[A-Z]/g, letter => "!" + letter.toLowerCase());

/**
 * Lays a file:// module proxy under `directory` serving `module` at
 * `version` out of `root`, and returns the directory to point GOPROXY at.
 */
export function layProxy({ root, directory, module, version }) {
  const at = join(directory, ...escapeModule(module).split("/"), "@v");
  mkdirSync(at, { recursive: true });
  writeFileSync(join(at, "list"), version + "\n");
  writeFileSync(join(at, version + ".info"), JSON.stringify({ Version: version, Time: "1980-01-01T00:00:00Z" }) + "\n");
  writeFileSync(join(at, version + ".mod"), readFileSync(join(root, "go.mod")));
  writeFileSync(join(at, version + ".zip"), moduleZip({ root, module, version }));
  return directory;
}

/** The module's own files, in path order: tracked and untracked-but-not-ignored, less every nested module's subtree. */
export function moduleFiles(root) {
  const listed = execFileSync("git", ["ls-files", "--cached", "--others", "--exclude-standard", "-z"], { cwd: root, encoding: "utf8", maxBuffer: 64 << 20 })
    .split("\0")
    .filter(Boolean)
    .sort();
  const nested = listed.filter(path => path !== "go.mod" && path.endsWith("/go.mod")).map(path => posix.dirname(path) + "/");
  return listed.filter(path => !nested.some(prefix => path.startsWith(prefix)));
}

/** The module zip: `<module>@<version>/<path>` for every file of the module. */
export function moduleZip({ root, module, version }) {
  const prefix = `${module}@${version}/`;
  const local = [];
  const central = [];
  let offset = 0;
  let count = 0;
  for (const path of moduleFiles(root)) {
    const file = join(root, path);
    if (!statSync(file).isFile()) continue;
    const name = Buffer.from(prefix + path, "utf8");
    const data = readFileSync(file);
    // Deflate unless it makes the entry larger, which it does for the short
    // JSON fixtures this repository is full of.
    const deflated = deflateRawSync(data, { level: 9 });
    const stored = deflated.length >= data.length;
    const body = stored ? data : deflated;
    const sum = crc32(data);
    local.push(header(0x04034b50, 30, view => {
      view.writeUInt16LE(20, 4);
      view.writeUInt16LE(stored ? 0 : 8, 8);
      view.writeUInt16LE(0x0021, 12); // 1980-01-01: a module zip carries no history and none is read.
      view.writeUInt32LE(sum, 14);
      view.writeUInt32LE(body.length, 18);
      view.writeUInt32LE(data.length, 22);
      view.writeUInt16LE(name.length, 26);
    }), name, body);
    central.push(header(0x02014b50, 46, view => {
      view.writeUInt16LE(20, 4);
      view.writeUInt16LE(20, 6);
      view.writeUInt16LE(stored ? 0 : 8, 10);
      view.writeUInt16LE(0x0021, 14);
      view.writeUInt32LE(sum, 16);
      view.writeUInt32LE(body.length, 20);
      view.writeUInt32LE(data.length, 24);
      view.writeUInt16LE(name.length, 28);
      view.writeUInt32LE(offset, 42);
    }), name);
    offset += 30 + name.length + body.length;
    count++;
  }
  const directory = Buffer.concat(central);
  const end = header(0x06054b50, 22, view => {
    view.writeUInt16LE(count, 8);
    view.writeUInt16LE(count, 10);
    view.writeUInt32LE(directory.length, 12);
    view.writeUInt32LE(offset, 16);
  });
  return Buffer.concat([...local, directory, end]);
}

/** One zip record: a fixed-size header with its signature, filled in by `fill`, and whatever follows it. */
function header(signature, size, fill, ...rest) {
  const view = Buffer.alloc(size);
  view.writeUInt32LE(signature, 0);
  fill(view);
  return rest.length ? Buffer.concat([view, ...rest]) : view;
}
