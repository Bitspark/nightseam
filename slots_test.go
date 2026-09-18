package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Bitspark/nighthall/tools/go/generate-api/internal/contract"
	"github.com/Bitspark/nighthall/tools/go/generate-api/internal/kernel"
)

// carrierContract carries the probe family: a frame holding one of its
// envelopes, an attachment holding a handle to a channel that speaks it, a
// method that returns one of its envelopes by name, and an event of frames.
const carrierContract = `{
 "schema_version":1,"profile":"nighthall.duplex/1","name":"carrier",
 "types":{
  "Frame":{"kind":"record","fields":[{"name":"sequence","type":"integer"},{"name":"message","type":{"envelope":"session"}}]},
  "Attachment":{"kind":"record","fields":[{"name":"connection","type":{"connection":"session"}},{"name":"last","type":"integer"}]},
  "AttachParams":{"kind":"record","fields":[{"name":"id","type":"string","go_name":"ID"}]},
  "Frames":{"kind":"alias","type":{"array":"Frame"}}
 },
 "methods":[
  {"name":"attach","go_name":"Attach","ts_name":"attach","direction":"client_to_server","request":"AttachParams","result":"Attachment"},
  {"name":"relay","go_name":"Relay","ts_name":"relay","direction":"client_to_server","request":"Frame","result":{"envelope":"probe"}}
 ],
 "events":[{"name":"frame.relayed","go_name":"FrameRelayed","ts_name":"frameRelayed","direction":"server_to_client","type":"Frame"}],
 "errors":[]
}`

// slotWorld is the probe family, declared a session family, and the
// carrier, with the carrier substituted with probe: the left path.
func slotWorld(t *testing.T) (world kernel.World, probe, substituted map[string]any) {
	t.Helper()
	probe = exampleAPI(t)
	probe["role"] = contract.SessionRole
	var carrier map[string]any
	if err := json.Unmarshal([]byte(carrierContract), &carrier); err != nil {
		t.Fatal(err)
	}
	world = kernel.World{"probe": probe, "carrier": carrier}
	return world, probe, contract.Substitute(carrier, "probe")
}

// renderSlotFixture renders probe and the substituted carrier into a
// temporary module beside the copied runtime.
func renderSlotFixture(t *testing.T, directory, root string) {
	t.Helper()
	world, probe, substituted := slotWorld(t)
	for _, input := range []map[string]any{probe, substituted} {
		result, err := kernel.GenerateIn(world, input, languages("example.test/generated")...)
		if err != nil {
			t.Fatal(err)
		}
		for p, data := range result.Files {
			writeFixture(t, directory, p, data)
		}
	}
	copyFixtureTree(t, filepath.Join(root, "api/go/ws-runtime"), filepath.Join(directory, "api/go/ws-runtime"))
	writeFixture(t, directory, "go.mod", []byte("module example.test/generated\n\ngo 1.25.0\n\nrequire (\n\tgithub.com/Bitspark/nighthall v0.0.0\n\tgithub.com/coder/websocket v1.8.15\n)\n\nreplace github.com/Bitspark/nighthall => "+filepath.ToSlash(root)+"\n"))
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "go.sum", sum)
}

// TestSlottedContractIsRefusedUntilSubstituted: the carrier as written has
// no rendering; the kernel says so at every slot.
func TestSlottedContractIsRefusedUntilSubstituted(t *testing.T) {
	world, _, _ := slotWorld(t)
	diagnostics := kernel.ValidateIn(world, world["carrier"], languages(module)...)
	unsupported := 0
	for _, d := range diagnostics {
		if d.Code == "unsupported_slot" {
			unsupported++
		}
	}
	if unsupported != 3 || len(diagnostics) != 3 {
		t.Fatalf("expected exactly three unsupported slots, got %+v", diagnostics)
	}
	if _, err := kernel.GenerateIn(world, world["carrier"], languages(module)...); err == nil {
		t.Fatal("a slotted contract rendered")
	}
}

// TestSubstitutedCarrierRendersReferencingProbe: the left path renders, and
// what it renders refers to probe's Envelope and Handle by import, in Go
// and in TypeScript. The exported surface of the carrier's Go protocol
// package is the golden below: the instantiation of a generic rendering,
// when one exists, must reproduce it exactly.
func TestSubstitutedCarrierRendersReferencingProbe(t *testing.T) {
	world, _, substituted := slotWorld(t)
	result, err := kernel.GenerateIn(world, substituted, languages(module)...)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for p, data := range result.Files {
		files[p] = string(data)
	}
	for path, wants := range map[string][]string{
		"api/go/carrier-protocol/types_generated.go":      {`probeprotocol "github.com/Bitspark/nighthall/api/go/probe-protocol"`},
		"api/go/carrier-protocol/validation_generated.go": {`"probe": probeprotocol.ValidateRaw`},
		"api/go/carrier-client/client_generated.go":       {"(probeprotocol.Envelope, error)"},
		"api/ts/carrier-client/src/types.ts":              {`import type * as probe from "@nighthall/probe-client";`, `import { validateWire as validate_probe } from "@nighthall/probe-client";`, `"message": probe.Envelope;`, `"connection": probe.Handle;`, `"probe": validate_probe`},
		"api/ts/carrier-client/src/index.ts":              {"Promise<probe.Envelope>"},
		"api/ts/carrier-client/package.json":              {`"@nighthall/probe-client":"0.0.0"`},
	} {
		for _, want := range wants {
			if !strings.Contains(files[path], want) {
				t.Errorf("%s lacks %s", path, want)
			}
		}
	}
	golden := []string{
		"AttachParams{ID string}",
		"Attachment{Connection probeprotocol.Handle, Last int64}",
		"Envelope{Version int64, Kind string, ID wsruntime.Optional[string], Method wsruntime.Optional[string], Params wsruntime.Optional[any], Result wsruntime.Optional[any], Error wsruntime.Optional[any], Event wsruntime.Optional[string], Data wsruntime.Optional[any]}",
		"Frames = []Frame",
		"Frame{Sequence int64, Message probeprotocol.Envelope}",
		"Handle{Channel int64}",
	}
	if got := surface(t, files["api/go/carrier-protocol/types_generated.go"]); strings.Join(got, "\n") != strings.Join(golden, "\n") {
		t.Fatalf("the carrier's Go surface is not the golden one:\n%s", strings.Join(got, "\n"))
	}
}

// surface lists the exported types of a generated Go file: each record with
// its fields and their types, each alias with its target, sorted by name.
func surface(t *testing.T, source string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "types_generated.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.TypeSpec)
		if !ok || !spec.Name.IsExported() {
			return true
		}
		switch typed := spec.Type.(type) {
		case *ast.StructType:
			var fields []string
			for _, field := range typed.Fields.List {
				for _, name := range field.Names {
					if name.IsExported() {
						fields = append(fields, name.Name+" "+exprString(field.Type))
					}
				}
			}
			out = append(out, spec.Name.Name+"{"+strings.Join(fields, ", ")+"}")
		default:
			out = append(out, spec.Name.Name+" = "+exprString(spec.Type))
		}
		return true
	})
	sort.Strings(out)
	return out
}

func exprString(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return exprString(typed.X) + "." + typed.Sel.Name
	case *ast.ArrayType:
		return "[]" + exprString(typed.Elt)
	case *ast.MapType:
		return "map[" + exprString(typed.Key) + "]" + exprString(typed.Value)
	case *ast.IndexExpr:
		return exprString(typed.X) + "[" + exprString(typed.Index) + "]"
	case *ast.StarExpr:
		return "*" + exprString(typed.X)
	case *ast.InterfaceType:
		return "any"
	}
	return "?"
}

// TestSubstitutedCarrierCompilesAndDelegatesValidation: both families
// compile in one module, and a carrier value holding a probe envelope is
// validated by probe's validator through the carrier's.
func TestSubstitutedCarrierCompilesAndDelegatesValidation(t *testing.T) {
	root := repositoryRoot(t)
	directory := t.TempDir()
	renderSlotFixture(t, directory, root)
	writeFixture(t, directory, "delegation_test.go", []byte(`package generated
import ("testing";carrier "example.test/generated/api/go/carrier-protocol")
func TestDelegation(t *testing.T){
 if err:=carrier.ValidateRaw("Frame",[]byte(`+"`"+`{"sequence":1,"message":{"version":1,"kind":"event","event":"changed","data":{}}}`+"`"+`));err!=nil{t.Fatal(err)}
 if err:=carrier.ValidateRaw("Frame",[]byte(`+"`"+`{"sequence":1,"message":{"version":1}}`+"`"+`));err==nil{t.Fatal("an envelope without a kind passed")}
 if err:=carrier.ValidateRaw("Frame",[]byte(`+"`"+`{"sequence":1,"message":{"version":1,"kind":"event","extra":true}}`+"`"+`));err==nil{t.Fatal("an unknown envelope field passed")}
 if err:=carrier.ValidateExpressionRaw("probe.Nope",[]byte("{}"));err==nil{t.Fatal("an unknown imported type passed")}
 if err:=carrier.ValidateExpressionRaw("nobody.Envelope",[]byte("{}"));err==nil{t.Fatal("an unknown family passed")}
}`))
	runFixture(t, directory, "go", "test", "-count=1", "./...")
}

// TestSubstitutedCarrierTypeChecksInTypeScript: the carrier's TypeScript
// client compiles against probe's, and its validator delegates to probe's.
func TestSubstitutedCarrierTypeChecksInTypeScript(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node is not installed")
	}
	root := repositoryRoot(t)
	tsc := filepath.Join(root, "node_modules/typescript/bin/tsc")
	if _, err := os.Stat(tsc); err != nil {
		t.Skip("TypeScript parser is not installed")
	}
	directory := t.TempDir()
	renderSlotFixture(t, directory, root)
	copyFixtureTree(t, filepath.Join(root, "api/ts/ws-runtime"), filepath.Join(directory, "api/ts/ws-runtime"))
	config := map[string]any{"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": map[string]any{"@nighthall/ws-runtime": []string{"./api/ts/ws-runtime/src/index.ts"}, "@nighthall/probe-client": []string{"./api/ts/probe-client/src/index.ts"}}}, "include": []string{"api/ts/**/*.ts"}}
	data, _ := json.Marshal(config)
	writeFixture(t, directory, "tsconfig.json", data)
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	writeFixture(t, directory, "loader.mjs", []byte(`export async function resolve(specifier,context,next){const map={'@nighthall/ws-runtime':'./api/ts/ws-runtime/src/index.ts','@nighthall/probe-client':'./api/ts/probe-client/src/index.ts'};if(map[specifier])return {url:new URL(map[specifier],import.meta.url).href,shortCircuit:true};return next(specifier,context);}`))
	writeFixture(t, directory, "delegation.mjs", []byte(`import assert from 'node:assert/strict';import {validateWire} from './api/ts/carrier-client/src/types.ts';
validateWire('Frame',{sequence:1,message:{version:1,kind:'event',event:'changed',data:{}}});
assert.throws(()=>validateWire('Frame',{sequence:1,message:{version:1}}));
assert.throws(()=>validateWire('Frame',{sequence:1,message:{version:1,kind:'event',extra:true}}));
assert.throws(()=>validateWire('probe.Nope',{}));assert.throws(()=>validateWire('nobody.Envelope',{}));
`))
	runFixture(t, directory, "node", "--loader", "./loader.mjs", "delegation.mjs")
}
