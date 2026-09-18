package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/kernel"
	"github.com/Bitspark/nightseam/internal/legacy/contract"
	legacy "github.com/Bitspark/nightseam/internal/legacy/kernel"
	legacyspi "github.com/Bitspark/nightseam/internal/legacy/spi"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/oracle"
	"github.com/Bitspark/nightseam/internal/targets/golang"
)

// A generation is one generator the slow fixtures run against: v1, the
// legacy generator on the contracts the tests declare inline, and v2 on
// the same families under testdata/v2/families. The fixture bodies — the
// hand-written Go tests and Node scripts that compile and run against the
// generated packages — are the same for both, which is what makes the two
// generators render the same API. Until v2 renders TypeScript, v2 renders
// the Go packages and v1 the TypeScript ones: a v2 Go peer speaks with a
// v1 TypeScript peer over the wire, as the profile promises.
type generation struct {
	name string
	// probe renders the probe family into the fixture directory.
	probe func(t *testing.T, directory string)
	// slots renders probe, the substituted carrier at the default paths —
	// the left path — and the carrier as written at gen/ — the right path.
	slots func(t *testing.T, directory string)
}

var generations = []generation{
	{name: "v1", probe: v1Probe, slots: v1Slots},
	{name: "v2", probe: v2Probe, slots: v2Slots},
}

func writeAll(t *testing.T, directory string, files map[string][]byte) {
	t.Helper()
	for p, data := range files {
		writeFixture(t, directory, p, data)
	}
}

func v1Probe(t *testing.T, directory string) {
	t.Helper()
	result, err := legacy.Generate(exampleAPI(t), languages(module, scope)...)
	if err != nil {
		t.Fatal(err)
	}
	writeAll(t, directory, result.Files)
}

func v1Slots(t *testing.T, directory string) {
	t.Helper()
	world, probe, substituted := slotWorld(t)
	render := func(input map[string]any, languages []legacyspi.Language) {
		result, err := legacy.GenerateIn(world, input, languages...)
		if err != nil {
			t.Fatal(err)
		}
		writeAll(t, directory, result.Files)
	}
	render(probe, languages(module, scope))
	render(substituted, languages(module, scope))
	render(world["carrier"], rightLanguages(module, scope))
}

// v2 renders the Go packages; the TypeScript ones come from v1 until v2
// renders them too.
func v2Probe(t *testing.T, directory string) {
	t.Helper()
	v1 := map[string][]byte{}
	result, err := legacy.Generate(exampleAPI(t), languages(module, scope)...)
	if err != nil {
		t.Fatal(err)
	}
	for p, data := range result.Files {
		if !strings.HasSuffix(p, ".go") {
			v1[p] = data
		}
	}
	writeAll(t, directory, v1)
	k := v2Kernel(module, scope)
	world := k.Load(os.DirFS(filepath.Join(v2Root, "families")), "api/contracts")
	rendered, err := k.Render(world, "probe")
	if err != nil {
		t.Fatal(err)
	}
	writeAll(t, directory, rendered.Files)
}

func v2Slots(t *testing.T, directory string) {
	t.Helper()
	// The TypeScript packages of every path, from v1.
	world, probe, substituted := slotWorld(t)
	for _, r := range []struct {
		input     map[string]any
		languages []legacyspi.Language
	}{{probe, languages(module, scope)}, {substituted, languages(module, scope)}, {world["carrier"], rightLanguages(module, scope)}} {
		result, err := legacy.GenerateIn(world, r.input, r.languages...)
		if err != nil {
			t.Fatal(err)
		}
		for p, data := range result.Files {
			if !strings.HasSuffix(p, ".go") {
				writeFixture(t, directory, p, data)
			}
		}
	}
	// The Go packages: probe and the substituted carrier at the default
	// layout, the carrier as written at gen/.
	families := kernel.Load(os.DirFS(filepath.Join(v2Root, "families")), "api/contracts", []string{"go", "typescript"})
	left := kernel.New(golang.New(golang.Config{Module: module}))
	bound := *families
	bound.Families = map[string]*model.Family{}
	for name, f := range families.Families {
		bound.Families[name] = f
	}
	bound.Families["carrier"] = oracle.Substitute(families.Families["carrier"], map[string]string{"S": "probe"})
	for _, name := range []string{"probe", "carrier"} {
		result, err := left.Render(&bound, name)
		if err != nil {
			t.Fatalf("left path, %s: %v", name, err)
		}
		writeAll(t, directory, result.Files)
	}
	// The carrier as written is placed at gen/, and refers to probe where
	// the layout puts it, beside the left path's.
	right := kernel.New(golang.New(golang.Config{Module: module, Place: map[string]golang.Layout{"carrier": {Protocol: "gen/go/{family}-protocol", Binding: "gen/go/{family}-binding", Client: "gen/go/{family}-client"}}}))
	result, err := right.Render(families, "carrier")
	if err != nil {
		t.Fatalf("right path: %v", err)
	}
	writeAll(t, directory, result.Files)
}

var _ = contract.SessionRole
