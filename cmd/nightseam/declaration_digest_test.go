package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// These revisions leave the family's validation descriptor unchanged: one
// changes only an ordinary method, the other only a reachable imported type.
// Compile and execute both targets so their public strings, including quoting
// and reexports, are held to the same canonical UTF-8 bytes.
func TestGeneratedDeclarationDigests(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	for _, scenario := range []struct {
		name      string
		base      map[string]string
		revision  map[string]string
		editorial map[string]string
	}{
		{
			name: "ordinary method only",
			base: map[string]string{
				"same/model.json":    `{"nightseam":2}`,
				"same/protocol.json": `{"profile":"nightseam.duplex/1","server":{"methods":{"echo":{"result":"string"}}}}`,
			},
			revision: map[string]string{
				"same/protocol.json": `{"profile":"nightseam.duplex/1","server":{"methods":{"echo":{"result":"integer"}}}}`,
			},
			editorial: map[string]string{
				"same/protocol.json":   "{\n  \"server\": {\"methods\": {\"echo\": {\"result\": \"string\", \"description\": \"New documentation.\"}}},\n  \"profile\": \"nightseam.duplex/1\"\n}",
				"same/go.json":         `{"names":{"echo":"RenamedEcho"}}`,
				"same/typescript.json": `{"names":{"echo":"renamedEcho"}}`,
			},
		},
		{
			name: "reachable import",
			base: map[string]string{
				"same/model.json":    `{"nightseam":2,"imports":["shared"],"types":{"Box":{"kind":"record","fields":[{"name":"payload","type":"shared.Payload"}]}}}`,
				"same/protocol.json": `{"profile":"nightseam.duplex/1","server":{"methods":{"echo":{"request":"Box","result":"Box"}}}}`,
				"shared/model.json":  `{"nightseam":2,"types":{"Payload":{"kind":"record","fields":[{"name":"value","type":"string"}]},"Unused":{"kind":"record","fields":[]}}}`,
			},
			revision: map[string]string{
				"shared/model.json": `{"nightseam":2,"types":{"Payload":{"kind":"record","fields":[{"name":"value","type":"integer"}]},"Unused":{"kind":"record","fields":[]}}}`,
			},
			editorial: map[string]string{
				"shared/model.json":      "{\n  \"nightseam\": 2,\n  \"types\": {\"Unused\": {\"kind\": \"record\", \"fields\": [{\"name\": \"extra\", \"type\": \"integer\"}]}, \"Payload\": {\"description\": \"Updated documentation.\", \"fields\": [{\"type\": \"string\", \"name\": \"value\"}], \"kind\": \"record\"}}\n}",
				"shared/go.json":         `{"names":{"Payload":"RenamedPayload","Payload.value":"RenamedValue"}}`,
				"shared/typescript.json": `{"names":{"Payload":"RenamedPayload","Payload.value":"renamedValue"}}`,
				"same/go.json":           `{"names":{"Box":"RenamedBox","echo":"RenamedEcho"}}`,
				"same/typescript.json":   `{"names":{"Box":"RenamedBox","echo":"renamedEcho"}}`,
			},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var baseline declarationExport
			for _, version := range []struct {
				name    string
				changes map[string]string
			}{
				{"baseline", nil},
				{"revision", scenario.revision},
				{"editorial", scenario.editorial},
			} {
				t.Run(version.name, func(t *testing.T) {
					directory := t.TempDir()
					for name, source := range scenario.base {
						writeFixture(t, directory, "api/contracts/"+name, []byte(source))
					}
					for name, source := range version.changes {
						writeFixture(t, directory, "api/contracts/"+name, []byte(source))
					}
					got := generatedDeclarationExport(t, directory, root, tsc)
					switch version.name {
					case "baseline":
						baseline = got
					case "revision":
						if got.descriptor != baseline.descriptor {
							t.Fatal("the counterexample changed the local validation descriptor")
						}
						if got.Declaration == baseline.Declaration || got.Digest == baseline.Digest {
							t.Fatal("a wire-visible revision did not change both the declaration and its digest")
						}
					case "editorial":
						if got != baseline {
							t.Fatalf("documentation, formatting, target names or unreachable declarations changed identity:\nbase: %+v\ngot: %+v", baseline, got)
						}
					}
				})
			}
		})
	}
}

type declarationExport struct {
	Declaration string `json:"declaration"`
	Digest      string `json:"digest"`
	descriptor  string
}

func generatedDeclarationExport(t *testing.T, directory, root, tsc string) declarationExport {
	t.Helper()
	if _, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("generate: %v\n%s", err, errs)
	}
	fixtureModule(t, directory, root)
	for _, component := range []string{"runtime", "duplex", "tunnel", "live"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	config, err := json.Marshal(map[string]any{
		"compilerOptions": map[string]any{
			"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true,
			"skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true,
			"paths": fixtureTypeScriptPaths(t, directory), "types": []string{"node"},
			"typeRoots": []string{filepath.ToSlash(filepath.Join(root, "node_modules/@types"))},
		},
		"include": []string{"api/ts/**/*.ts", "declaration.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.json", config)
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
	writeFixture(t, directory, "declaration/main.go", []byte(goDeclarationExportFixture))
	writeFixture(t, directory, "declaration.ts", []byte(tsDeclarationExportFixture))
	runFixture(t, directory, "go", "run", "./declaration")
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "declaration.ts")
	var exports [2]declarationExport
	for i, name := range []string{"go-declaration.json", "ts-declaration.json"} {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &exports[i]); err != nil {
			t.Fatal(err)
		}
		if !json.Valid([]byte(exports[i].Declaration)) {
			t.Fatalf("%s did not export a JSON declaration: %q", name, exports[i].Declaration)
		}
		digest := sha256.Sum256([]byte(exports[i].Declaration))
		if want := hex.EncodeToString(digest[:]); exports[i].Digest != want {
			t.Fatalf("%s digest %q does not hash its exact exported UTF-8 string; want %s", name, exports[i].Digest, want)
		}
	}
	if exports[0] != exports[1] {
		t.Fatalf("Go and TypeScript exported different declarations or digests: %+v", exports)
	}
	// The descriptor is intentionally a distinct artifact. Read its generated
	// literal to prove these revisions expose the old descriptor-only gap.
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(directory, "api/go/same-protocol/validation_generated.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "MustSchema" {
			return true
		}
		literal, ok := call.Args[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			t.Fatal("generated schema has no literal descriptor")
		}
		exports[0].descriptor, err = strconv.Unquote(literal.Value)
		if err != nil {
			t.Fatal(err)
		}
		return false
	})
	if exports[0].descriptor == "" {
		t.Fatal("generated schema has no descriptor")
	}
	return exports[0]
}

const goDeclarationExportFixture = `package main

import (
	"encoding/json"
	"os"
	protocol "example.test/generated/api/go/same-protocol"
)

func main() {
	digest, err := protocol.WireSchema().DeclarationDigest()
	if err != nil { panic(err) }
	if digest != protocol.WireDigest() { panic("schema lost the canonical declaration") }
	data, err := json.Marshal(map[string]string{"declaration": protocol.WireDeclaration(), "digest": protocol.WireDigest()})
	if err != nil { panic(err) }
	if err := os.WriteFile("go-declaration.json", data, 0600); err != nil { panic(err) }
}
`

const tsDeclarationExportFixture = `import { createHash } from "node:crypto";
import { writeFileSync } from "node:fs";
import { declarationDigest } from "@nightseam/runtime";
import { validateWire, wireDeclaration, wireDigest } from "@example/same-client";
import { wireDeclaration as bindingDeclaration, wireDigest as bindingDigest } from "@example/same-binding";

if (bindingDeclaration !== wireDeclaration || bindingDigest !== wireDigest) throw new Error("binding exports differ from client exports");
if (declarationDigest(validateWire) !== wireDigest) throw new Error("validator lost the canonical declaration");
JSON.parse(wireDeclaration);
if (createHash("sha256").update(wireDeclaration, "utf8").digest("hex") !== wireDigest) throw new Error("digest does not hash the exact exported UTF-8 string");
writeFileSync("ts-declaration.json", JSON.stringify({ declaration: wireDeclaration, digest: wireDigest }));
`
