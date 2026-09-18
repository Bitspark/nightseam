package main

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/legacy/contract"
	legacy "github.com/Bitspark/nightseam/internal/legacy/kernel"
)

// Until the legacy generator is deleted, the v1 corpus under
// testdata/corpus-v1 is rendered by it in-process, and what v2 renders for
// the converted corpus is held to it: the same exported Go surface,
// identifier for identifier, and the same TypeScript exports, name for
// name. The converter is what relates the two, so this is what proves it
// converts a family into the same family.

const legacyCorpusRoot = "testdata/corpus-v1"

// renderV1 renders every family of a layer-file checkout with the legacy
// generator, as the v1 tool did.
func renderV1(t *testing.T, root string) map[string][]byte {
	t.Helper()
	a := &app{root: root}
	names, err := a.layerFamilies()
	if err != nil {
		t.Fatal(err)
	}
	world := legacy.World{}
	for _, name := range names {
		sources, err := a.layerSources(name)
		if err != nil {
			t.Fatal(err)
		}
		files := map[string]map[string]any{}
		for layer, data := range sources {
			var file map[string]any
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.UseNumber()
			if err := decoder.Decode(&file); err != nil {
				t.Fatal(err)
			}
			files[layer] = file
		}
		merged, diagnostics := contract.Merge(files)
		if len(diagnostics) != 0 {
			t.Fatalf("%s: %v", name, diagnostics)
		}
		world[name] = merged
	}
	files := map[string][]byte{}
	for _, name := range names {
		result, err := legacy.GenerateIn(world, world[name], languages(module, scope)...)
		if err != nil {
			t.Fatal(err)
		}
		for p, data := range result.Files {
			files[p] = data
		}
	}
	return files
}

// renderV2 renders every family of a checkout with the tool as composed.
func renderV2(t *testing.T, root string) map[string][]byte {
	t.Helper()
	a := &app{root: root, module: module, scope: scope}
	names, err := a.chosen(nil)
	if err != nil {
		t.Fatal(err)
	}
	files, err := a.render(names)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestUpgradeIsSurfaceEquivalent: the exported Go surface of every package
// v2 renders for the converted corpus is identical to what v1 rendered for
// the corpus it was converted from, so that a consumer of the one is a
// consumer of the other.
func TestUpgradeIsSurfaceEquivalent(t *testing.T) {
	v1, v2 := renderV1(t, legacyCorpusRoot), renderV2(t, corpusRoot)
	for p, data := range v1 {
		if !strings.HasSuffix(p, ".go") {
			continue
		}
		rendered, ok := v2[p]
		if !ok {
			t.Errorf("v2 does not render %s", p)
			continue
		}
		want := strings.Join(goSurface(t, p, string(data)), "\n")
		got := strings.Join(goSurface(t, p, string(rendered)), "\n")
		if want != got {
			t.Errorf("the surface of %s differs:\n%s", p, diff(want, got))
		}
	}
}

// tsExports lists what a TypeScript module exports, by name, sorted: the
// surface a consumer sees, apart from the types' shapes, which the diagram
// fixture holds under tsc.
func tsExports(source string) []string {
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^export (?:interface|type|class|const|function|async function) ([A-Za-z_$][A-Za-z0-9_$]*)`).FindAllStringSubmatch(source, -1) {
		seen[m[1]] = true
	}
	for _, m := range regexp.MustCompile(`(?m)^export (?:type )?\{ ([^}]*) \}`).FindAllStringSubmatch(source, -1) {
		for _, name := range strings.Split(m[1], ",") {
			seen[strings.TrimSpace(name)] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TestUpgradeIsExportEquivalent: every TypeScript module v2 renders for the
// converted corpus exports what v1's does, name for name, so that a
// consumer of the one is a consumer of the other; what v2 exports beyond
// that is reported.
func TestUpgradeIsExportEquivalent(t *testing.T) {
	v1, v2 := renderV1(t, legacyCorpusRoot), renderV2(t, corpusRoot)
	for p, data := range v1 {
		if !strings.HasSuffix(p, ".ts") {
			continue
		}
		rendered, ok := v2[p]
		if !ok {
			t.Errorf("v2 does not render %s", p)
			continue
		}
		exported := map[string]bool{}
		for _, name := range tsExports(string(rendered)) {
			exported[name] = true
		}
		for _, name := range tsExports(string(data)) {
			if !exported[name] {
				t.Errorf("%s no longer exports %s", p, name)
			}
			delete(exported, name)
		}
		for name := range exported {
			t.Logf("%s also exports %s", p, name)
		}
	}
}
