// Build the source distributions as an unrelated consumer outside the checkout.
import { mkdtempSync, mkdirSync, writeFileSync, readdirSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import { checkHaskellVersions } from "./haskell-packages.mjs";

const root = resolve(fileURLToPath(new URL("..", import.meta.url)));
const version = JSON.parse(readFileSync(join(root, "runtime/ts/package.json"), "utf8")).version;
const problems = checkHaskellVersions(root, version);
if (problems.length) throw new Error(problems.join("\n"));
const scratch = mkdtempSync(join(tmpdir(), "nightseam-haskell-install-"));
function run(argv, cwd = root) {
  const child = spawnSync(argv[0], argv.slice(1), { cwd, stdio: "inherit", windowsHide: true });
  if (child.error) throw child.error;
  if (child.status !== 0) throw new Error(`${argv.join(" ")} exited ${child.status}`);
}
try {
  const archives = join(scratch, "archives");
  mkdirSync(archives);
  run(["stack", "--stack-yaml", join(root, "conformance/haskell/stack.yaml"), "sdist", "duplex/hs", "runtime/hs", "--tar-dir", archives]);
  const files = readdirSync(archives).filter(name => name.endsWith(".tar.gz"));
  if (files.length !== 2) throw new Error("Expected both Haskell source distributions");
  for (const file of files) run(["tar", "-xzf", join(archives, file), "-C", scratch]);
  writeFileSync(join(scratch, "stack.yaml"), `resolver: lts-22.44
system-ghc: true
packages:
  - .
  - nightseam-duplex-${version}
  - nightseam-runtime-${version}
`);
  writeFileSync(join(scratch, "nightseam-consumer-smoke.cabal"), `cabal-version: 2.4
name: nightseam-consumer-smoke
version: 0.0.0
build-type: Simple
executable consumer
  main-is: Main.hs
  build-depends: base, aeson, nightseam-duplex, nightseam-runtime
  default-language: Haskell2010
  ghc-options: -threaded
`);
  writeFileSync(join(scratch, "Main.hs"), `{-# LANGUAGE OverloadedStrings #-}
module Main where
import Control.Monad
import Data.Aeson hiding (defaultOptions, Options)
import Nightseam.Duplex
import qualified Nightseam.Duplex.WebSocket as WS
import Nightseam.Runtime
main :: IO ()
main = do
  listener <- WS.listen 1048576 []
  connection <- WS.dial (WS.listenerURL listener) 1048576 []
  accepted <- WS.accept listener
  client <- newPeer connection Client defaultOptions
  server <- newPeer accepted Server defaultOptions
  handle server "echo" (\\_ _ value -> pure value)
  ctx <- newCallContext
  answer <- call client ctx "echo" (object ["installed" .= True])
  unless (answer == object ["installed" .= True]) (error "installed request failed")
  closePeer client
  closePeer server
  WS.closeListener listener
  putStrLn "standalone Haskell request passed"
`);
  run(["stack", "--stack-yaml", join(scratch, "stack.yaml"), "build", "nightseam-consumer-smoke"], scratch);
  run(["stack", "--stack-yaml", join(scratch, "stack.yaml"), "exec", "consumer"], scratch);
} finally {
  if (relative(resolve(tmpdir()), resolve(scratch)).startsWith("..") || !basename(scratch).startsWith("nightseam-haskell-install-")) {
    throw new Error("Refusing cleanup outside the smoke directory");
  }
  rmSync(scratch, { recursive: true, force: true });
}
