package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSiblingResolutionOption(t *testing.T) {
	for mode, want := range map[string]string{"file": "file:../probe-client", "workspace": "workspace:*", "version": "0.0.0"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			copyFixtureTree(t, filepath.Join(corpusRoot, "api/contracts"), filepath.Join(dir, "api/contracts"))
			if _, _, err := run(t, dir, "generate", "--ts-sibling", mode); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "api/ts/carrier-client/package.json"))
			if err != nil {
				t.Fatal(err)
			}
			var manifest struct{ Dependencies map[string]string }
			if err := json.Unmarshal(data, &manifest); err != nil {
				t.Fatal(err)
			}
			if got := manifest.Dependencies["@example/probe-client"]; got != want {
				t.Fatalf("dependency = %q, want %q", got, want)
			}
			if _, _, err := run(t, dir, "check", "--ts-sibling", mode); err != nil {
				t.Fatal(err)
			}
		})
	}
	if out, errs, err := run(t, corpusRoot, "validate", "--ts-sibling", "invalid"); err == nil || !strings.Contains(out+errs+err.Error(), "sibling") {
		t.Fatalf("invalid mode: %v", err)
	}
}

// Real package-manager resolution catches mistakes that compiling through
// the fixture's TypeScript paths mappings cannot: a generated sibling must
// be installed locally even when pnpm does not link workspace versions.
func TestGeneratedSiblingsInstallLocally(t *testing.T) {
	root := repositoryRoot(t)
	fixture(t, root, "node", "npm", "pnpm")
	for _, manager := range []string{"npm", "pnpm"} {
		t.Run(manager, func(t *testing.T) {
			dir := t.TempDir()
			writeAll(t, dir, renderTool(t, corpusRoot))
			manifestData, err := os.ReadFile(filepath.Join(dir, "api/ts/probe-client/package.json"))
			if err != nil {
				t.Fatal(err)
			}
			var manifest struct{ Dependencies map[string]string }
			if err := json.Unmarshal(manifestData, &manifest); err != nil {
				t.Fatal(err)
			}
			writeFixture(t, dir, "package.json", []byte(`{"name":"sibling-install","private":true,"workspaces":["api/ts/*","stubs/*"]}`))
			writeFixture(t, dir, "pnpm-workspace.yaml", []byte("packages:\n  - 'api/ts/*'\n  - 'stubs/*'\nlinkWorkspacePackages: false\noverrides:\n  '@nightseam/runtime': 'workspace:*'\n  '@nightseam/tunnel': 'workspace:*'\n"))
			for _, name := range []string{"runtime", "tunnel"} {
				writeFixture(t, dir, "stubs/"+name+"/package.json", []byte(`{"name":"@nightseam/`+name+`","version":"`+manifest.Dependencies["@nightseam/"+name]+`","private":true}`))
			}
			writeFixture(t, dir, "install.mjs", []byte(`import {spawnSync} from 'node:child_process';
import {createRequire} from 'node:module';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
const manager=process.argv[2];
const args=['install','--offline','--ignore-scripts',...(manager==='npm'?['--no-audit','--no-fund']:['--no-frozen-lockfile'])];
const result=spawnSync(manager,args,{stdio:'inherit',shell:process.platform==='win32'});
if(result.status!==0)process.exit(result.status??1);
const carrier=createRequire(new URL('./api/ts/carrier-client/package.json',import.meta.url));
const album=createRequire(new URL('./api/ts/album-client/package.json',import.meta.url));
const installedCarrier=album.resolve('@example/carrier-client');
assert.equal(readFileSync(carrier.resolve('@example/probe-client'),'utf8'),readFileSync('api/ts/probe-client/src/index.ts','utf8'));
assert.equal(readFileSync(installedCarrier,'utf8'),readFileSync('api/ts/carrier-client/src/index.ts','utf8'));
assert.equal(readFileSync(createRequire(installedCarrier).resolve('@example/probe-client'),'utf8'),readFileSync('api/ts/probe-client/src/index.ts','utf8'));
`))
			runFixture(t, dir, "node", "install.mjs", manager)
		})
	}
}
