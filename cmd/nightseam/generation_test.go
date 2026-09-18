package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/kernel"
	legacy "github.com/Bitspark/nightseam/internal/legacy/kernel"
	legacyspi "github.com/Bitspark/nightseam/internal/legacy/spi"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/oracle"
	"github.com/Bitspark/nightseam/internal/targets/golang"
	"github.com/Bitspark/nightseam/internal/targets/typescript"
)

// A generation is one generator the slow fixtures run against: v1, the
// legacy generator on the contracts the tests declare inline, and v2 on
// the same families under testdata/v2/families. The fixture bodies — the
// hand-written Go tests and Node scripts that compile and run against the
// generated packages — are the same for both, which is what makes the two
// generators render the same API.
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

func v2Probe(t *testing.T, directory string) {
	t.Helper()
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
	// The world is probe and the carrier alone, as v1's is: with another
	// session family in it the carrier's client would refer to that one too.
	loaded := kernel.Load(os.DirFS(filepath.Join(v2Root, "families")), "api/contracts", []string{"go", "typescript"})
	families := &kernel.World{Families: map[string]*model.Family{"probe": loaded.Families["probe"], "carrier": loaded.Families["carrier"]}, Names: []string{"carrier", "probe"}, Problems: map[string][]diag.Diagnostic{}}
	// The left path: probe and the carrier with S bound to probe, at the
	// default layout.
	left := v2Kernel(module, scope)
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
	// The right path: the carrier as written, placed at gen/, referring to
	// probe where the layout puts it, beside the left path's.
	right := kernel.New(
		golang.New(golang.Config{Module: module, Place: map[string]golang.Layout{"carrier": {Protocol: "gen/go/{family}-protocol", Binding: "gen/go/{family}-binding", Client: "gen/go/{family}-client"}}}),
		typescript.New(typescript.Config{Scope: scope, Place: map[string]string{"carrier": "gen/ts/{family}-client"}}),
	)
	result, err := right.Render(families, "carrier")
	if err != nil {
		t.Fatalf("right path: %v", err)
	}
	writeAll(t, directory, result.Files)
}
