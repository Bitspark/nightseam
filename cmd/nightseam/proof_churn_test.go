package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/load"
)

// The #60 measurement moves the image member, keeping its inline shape whole,
// from Part's variants to RichPart's. The wire alternative survives in RichPart;
// its declaration path and therefore its public name change in both targets.
func movedProofSource(t *testing.T) fstest.MapFS {
	t.Helper()
	source := proofSource(t)
	path := "contracts/proof/model.json"
	var model map[string]any
	if err := json.Unmarshal(source[path].Data, &model); err != nil {
		t.Fatal(err)
	}
	types := model["types"].(map[string]any)
	base := types["Part"].(map[string]any)["variants"].(map[string]any)
	extended := types["RichPart"].(map[string]any)["variants"].(map[string]any)
	extended["image"] = base["image"]
	delete(base, "image")
	data, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	source[path] = &fstest.MapFile{Data: data}
	return source
}

func TestProofInlineMoveChurnGolden(t *testing.T) {
	k, _ := toolKernel(load.Config{}, module, scope, "")
	before, after := k.Load(proofSource(t), "contracts"), k.Load(movedProofSource(t), "contracts")
	names := func(world analysis.World) []string {
		var names []string
		for _, inline := range analysis.Resolve(world, "proof").Inlines() {
			names = append(names, inline.Name)
		}
		sort.Strings(names)
		return names
	}
	beforeNames, afterNames := names(analysis.World(before.Families)), names(analysis.World(after.Families))
	if !reflect.DeepEqual(beforeNames, []string{"OptionNone", "PartImage", "PartsRequest", "RichPartTable"}) || !reflect.DeepEqual(afterNames, []string{"OptionNone", "PartsRequest", "RichPartImage", "RichPartTable"}) {
		t.Fatalf("unexpected inline names before %v after %v", beforeNames, afterNames)
	}
	a, err := k.Render(before, "proof")
	if err != nil {
		t.Fatal(err)
	}
	b, err := k.Render(after, "proof")
	if err != nil {
		t.Fatal(err)
	}
	type change struct {
		Path    string   `json:"path"`
		Removed []string `json:"removed_public_declarations,omitempty"`
		Added   []string `json:"added_public_declarations,omitempty"`
	}
	var changes []change
	// TypeScript's exported declaration lines hold the derived names and the
	// union alternatives. Go uses the same AST reader as the surface gate.
	exported := regexp.MustCompile(`(?m)^export (?:interface|type|class|const|function) [^\n]+`)
	declarations := func(path string, data []byte) []string {
		if strings.HasSuffix(path, ".go") {
			return goSurface(t, path, string(data))
		}
		if strings.HasSuffix(path, ".ts") {
			return exported.FindAllString(string(data), -1)
		}
		return nil
	}
	difference := func(a, b []string) []string {
		seen := map[string]bool{}
		for _, v := range b {
			seen[v] = true
		}
		var out []string
		for _, v := range a {
			if !seen[v] {
				out = append(out, v)
			}
		}
		sort.Strings(out)
		return out
	}
	paths := map[string]bool{}
	for path := range a.Files {
		paths[path] = true
	}
	for path := range b.Files {
		paths[path] = true
	}
	for path := range paths {
		data := a.Files[path]
		if bytes.Equal(data, b.Files[path]) {
			continue
		}
		left, right := declarations(path, data), declarations(path, b.Files[path])
		changes = append(changes, change{path, difference(left, right), difference(right, left)})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	if len(changes) == 0 {
		t.Fatal("moving an inline shape changed nothing")
	}
	report := struct {
		Move    string   `json:"move"`
		Before  []string `json:"before_inline_names"`
		After   []string `json:"after_inline_names"`
		Changed []change `json:"changed_files"`
	}{"model.json#/types/Part/variants/image -> model.json#/types/RichPart/variants/image", beforeNames, afterNames, changes}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	holdGolden(t, "testdata/golden-proof-churn", map[string][]byte{"move-image.json": append(data, '\n')})
}

func TestProofInlineMoveStillCompiles(t *testing.T) {
	root := repositoryRoot(t)
	fixture(t, root, "go", "node", "tsc")
	k, _ := toolKernel(load.Config{}, module, scope, "")
	world := k.Load(movedProofSource(t), "contracts")
	directory := t.TempDir()
	for _, name := range []string{"probe", "proof"} {
		result, err := k.Render(world, name)
		if err != nil {
			t.Fatal(err)
		}
		writeAll(t, directory, result.Files)
	}
	fixtureModule(t, directory, root)
	runFixture(t, directory, "go", "test", "./...")
	typescriptLanguageFixture(t, analysis.World(world.Families), `
import type {RichPart,RichPartImage} from '@example/proof-client';
const image:RichPartImage={url:'https://example.org/image',alt:null};
const part:RichPart={type:'image',value:image};
// @ts-expect-error The old path-derived name is no longer exported.
import type {PartImage} from '@example/proof-client';
`, `
import assert from 'node:assert/strict';
import {validateWire} from '@example/proof-client';
const image={type:'image',value:{url:'https://example.org/image',alt:null}};
validateWire('RichPart',image);
assert.throws(()=>validateWire('Part',image));
`)
}
