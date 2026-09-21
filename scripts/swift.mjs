// Native Swift package checks and a consumer outside the repository. A missing
// Swift toolchain is an error, just like the conformance recipe requires.
import { spawnSync } from "node:child_process";
import { cpSync, mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
function swift(args) {
  const result = spawnSync("swift", args, { cwd: root, stdio: "inherit" });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`swift ${args[0]} failed (${result.status})`);
}

swift(["--version"]);
for (const component of ["duplex", "runtime"]) {
  swift(["test", "--package-path", join(root, component, "swift", component === "duplex" ? "NightseamDuplex" : "NightseamRuntime"), "--jobs", "2"]);
}

const scratch = mkdtempSync(join(tmpdir(), "nightseam-swift-consumer-"));
try {
  for (const component of ["duplex", "runtime"]) {
    const from = join(root, component, "swift");
    cpSync(from, join(scratch, component, "swift"), {
      recursive: true,
      filter: source => !source.split(/[\\/]/).some(part => [".build", ".swiftpm", "Tests"].includes(part)),
    });
  }
  const consumer = join(scratch, "consumer");
  mkdirSync(join(consumer, "Sources", "Smoke"), { recursive: true });
  writeFileSync(join(consumer, "Package.swift"), `// swift-tools-version: 6.0
import PackageDescription
let package = Package(name: "Consumer", platforms: [.macOS(.v13)], dependencies: [
  .package(path: "../runtime/swift/NightseamRuntime"),
  .package(path: "../duplex/swift/NightseamDuplex")
], targets: [.executableTarget(name: "Smoke", dependencies: [
  .product(name: "NightseamRuntime", package: "NightseamRuntime"),
  .product(name: "NightseamDuplex", package: "NightseamDuplex")
])], swiftLanguageModes: [.v6])
`);
  writeFileSync(join(consumer, "Sources", "Smoke", "Smoke.swift"), `import Foundation
import NightseamDuplex
import NightseamRuntime
@main struct Smoke {
  static func main() async throws {
    let pair = Pipe.pair()
    let server = try Peer(connection: pair.0, role: "server")
    let client = try Peer(connection: pair.1, role: "client")
    try await server.handle(method: "echo") { _, _, data in data }
    await server.start(); await client.start()
    let original = Data("{\\\"value\\\":1e3}".utf8)
    let reply = try await client.call(method: "echo", params: original)
    guard reply == original else { throw PublicError(code: "smoke", message: "Raw payload changed") }
    let wires = try wirePair()
    let detach = try handleWire(wires.1, path: ["cell", "read"]) { _, data in data }
    let local = try await callWire(at(wires.0, path: ["cell"]), path: ["read"], params: original)
    guard local == original else { throw PublicError(code: "smoke", message: "Wire payload changed") }
    detach(); try wires.0.close(code: 1000, reason: "done")
    await client.close(); await server.close()
    print("standalone Swift consumer passed")
  }
}
`);
  swift(["run", "--package-path", consumer, "--jobs", "2", "Smoke"]);
} finally {
  rmSync(scratch, { recursive: true, force: true });
}
