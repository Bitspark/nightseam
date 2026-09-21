package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The normal full tier compiles the actual generated browser and Go endpoint.
// Browser execution remains opt-in; CI does not acquire a Chromium dependency.
func TestIdentityBrowserFixtureCompiles(t *testing.T) {
	directory := buildIdentityBrowser(t)
	runFixture(t, directory, "go", "test", "-run", "^$", ".")
}

func buildIdentityBrowser(t *testing.T) string {
	t.Helper()
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	directory := t.TempDir()
	protocol := []byte(`{"profile":"nightseam.duplex/1","server":{"methods":{"echo":{"request":"Input","result":"string"}},"events":{"ready":{"type":"Input"}}},"client":{"methods":{"reverse":{"request":"Input","result":"string"}}}}`)
	for _, revision := range []struct{ root, scope, module, fields string }{
		{directory, "@example", module, `{"name":"value","type":"string"},{"name":"hint","type":"string","required":false}`},
		{filepath.Join(directory, "previous"), "@previous", module + "/previous", `{"name":"value","type":"string"}`},
	} {
		writeFixture(t, revision.root, "api/contracts/browser-identity/model.json", []byte(`{"nightseam":2,"types":{"Input":{"kind":"record","fields":[`+revision.fields+`]}}}`))
		writeFixture(t, revision.root, "api/contracts/browser-identity/protocol.json", protocol)
		if _, errs, err := run(t, revision.root, "--scope", revision.scope, "--module", revision.module, "generate"); err != nil {
			t.Fatalf("generate %s: %v\n%s", revision.scope, err, errs)
		}
	}
	for _, component := range []string{"runtime", "duplex", "tunnel", "live"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	for _, name := range []string{"browser.ts", "browser_host_test.go"} {
		data, err := os.ReadFile(filepath.Join(root, "cmd/nightseam/testdata/identity-browser", name))
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, directory, name, data)
	}
	paths := fixtureTypeScriptPaths(t, directory, "api/ts", "previous/api/ts")
	config, err := json.Marshal(map[string]any{
		"compilerOptions": map[string]any{
			"target": "ES2022", "module": "ESNext", "moduleResolution": "Bundler",
			"strict": true, "skipLibCheck": true, "rewriteRelativeImportExtensions": true,
			"rootDir": directory, "outDir": filepath.Join(directory, "site"), "types": []string{}, "paths": paths,
		},
		"files": []string{filepath.Join(directory, "browser.ts")},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.json", config)
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	imports := map[string]string{}
	for name, targets := range paths {
		imports[name] = "/" + strings.TrimSuffix(strings.TrimPrefix(targets[0], "./"), ".ts") + ".js"
	}
	importMap, err := json.Marshal(map[string]any{"imports": imports})
	if err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(root, "cmd/nightseam/testdata/identity-browser/index.html"))
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "site/index.html", []byte(strings.ReplaceAll(string(page), "__IMPORT_MAP__", string(importMap))))
	fixtureModule(t, directory, root)
	return directory
}

func TestIdentityBrowserExchange(t *testing.T) {
	if testing.Short() {
		t.Skip("browser execution belongs to the full tier")
	}
	browser := os.Getenv("NIGHTSEAM_BROWSER")
	if browser == "" {
		t.Skip("set NIGHTSEAM_BROWSER to an installed Chromium executable for native-browser evidence; see testdata/identity-browser/README.md")
	}
	if _, err := exec.LookPath(browser); err != nil {
		t.Fatalf("NIGHTSEAM_BROWSER: %v", err)
	}
	directory := buildIdentityBrowser(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "test", "-run", "^TestNativeBrowserGeneratedIdentity$", "-count=1", "-v", ".")
	command.Dir = directory
	command.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("native generated browser fixture: %v\n%s", err, output)
	}
	t.Logf("%s", output)
}
