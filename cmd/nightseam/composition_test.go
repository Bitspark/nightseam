package main

// The composition proof. It renders the declarations under
// testdata/compositions with the tool, lays the two languages' programs
// beside the packages it rendered, and runs them: Go against Go over a real
// socket, TypeScript against TypeScript, and TypeScript against Go across
// one. Nothing here is generated code edited by hand — the programs are
// written against the interfaces the generated packages declare, which is the
// only way a consumer is allowed to write them either.
//
// What it is for is docs/runtime/compositions.md: whether the live-reference
// behaviour v0.5.0 is to provide can be built out of the peers, tunnels and
// handles that already exist, and exactly where that basis stops short.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// compositionsRoot is relative to this package, which is where go test runs.
const compositionsRoot = "testdata/compositions"

func TestComposedLiveReferences(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	directory := t.TempDir()

	// The declarations, as a consumer keeps them, rendered by the tool.
	copyFixtureTree(t, filepath.FromSlash(compositionsRoot+"/api/contracts"), filepath.Join(directory, "api/contracts"))
	if out, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("generate: %v\n%s\n%s", err, out, errs)
	}

	// The two runtimes the generated packages bind to.
	fixtureModule(t, directory, root)
	for _, component := range []string{"runtime", "duplex", "tunnel"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	// The compiler's own node types come from the checkout that installed
	// them; the fixture has no node_modules of its own, and the packages it
	// checks reach each other through paths.
	writeFixture(t, directory, "tsconfig.json", fmt.Appendf(nil, compositionsTSConfig, filepath.ToSlash(filepath.Join(root, "node_modules/@types"))))

	// The programs, copied rather than inlined: they are ordinary source a
	// reader opens, gofmt holds and an editor understands.
	copyProgram(t, filepath.FromSlash(compositionsRoot+"/go"), directory, ".go")
	copyProgram(t, filepath.FromSlash(compositionsRoot+"/ts"), directory, ".ts")

	// The TypeScript is type-checked before it is run, and the data-only half
	// runs on its own: it needs no connection, which is the point of it.
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "data.ts")
	// Everything else: the Go cases over real sockets, and — inside
	// TestAcrossTheWire — a Node client of a Go worker over one more.
	runFixture(t, directory, "go", "test", "-count=1", ".")
}

// copyProgram lays one directory's files of a suffix flat into the rendered
// checkout, where the generated packages are importable by the module path
// the fixture rendered them under.
func copyProgram(t *testing.T, from, to, suffix string) {
	t.Helper()
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	laid := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != suffix {
			continue
		}
		data, err := os.ReadFile(filepath.Join(from, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, to, entry.Name(), data)
		laid++
	}
	if laid == 0 {
		t.Fatalf("no %s program under %s", suffix, from)
	}
}

const compositionsTSConfig = `{
	"compilerOptions": {
		"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext",
		"strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true,
		"types": ["node"],
		"typeRoots": ["%s"],
		"paths": {
			"@nightseam/runtime": ["./runtime/ts/src/index.ts"],
			"@nightseam/duplex": ["./duplex/ts/src/index.ts"],
			"@nightseam/tunnel": ["./tunnel/ts/src/index.ts"],
			"@example/*": ["./api/ts/*/src/index.ts"]
		}
	},
	"include": ["api/ts/**/*.ts", "compositions.ts", "data.ts"]
}`
