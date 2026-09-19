package main

import (
	"os"
	"testing"

	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/kernel"
	"github.com/Bitspark/nightseam/internal/load"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/oracle"
	"github.com/Bitspark/nightseam/internal/targets/golang"
	"github.com/Bitspark/nightseam/internal/targets/typescript"
)

// A generation is one generator the slow fixtures run against, on the
// families under testdata/families. The fixture bodies — the hand-written
// Go tests and Node scripts that compile and run against the generated
// packages — were written against the previous generator and pass
// unchanged against this one, which is what made the two render the same
// API; a next generation is held to them the same way.
type generation struct {
	name string
	// probe renders probe and the full-language proof into the fixture directory.
	probe func(t *testing.T, directory string)
	// slots renders probe, the substituted carrier at the default paths —
	// the left path — and the carrier as written at gen/ — the right path.
	slots func(t *testing.T, directory string)
}

var generations = []generation{
	{name: "tool", probe: toolProbe, slots: toolSlots},
}

func writeAll(t *testing.T, directory string, files map[string][]byte) {
	t.Helper()
	for p, data := range files {
		writeFixture(t, directory, p, data)
	}
}

func toolProbe(t *testing.T, directory string) {
	t.Helper()
	k, _ := toolKernel(load.Config{}, module, scope, "")
	world := k.Load(os.DirFS(familiesRoot), "api/contracts")
	for _, name := range []string{"probe", "proof"} {
		rendered, err := k.Render(world, name)
		if err != nil {
			t.Fatal(err)
		}
		writeAll(t, directory, rendered.Files)
	}
}

func toolSlots(t *testing.T, directory string) {
	t.Helper()
	// The world is probe and the carrier alone, as v1's is: with another
	// family in it the carrier's client would refer to that one too.
	loaded := kernel.Load(os.DirFS(familiesRoot), "api/contracts", []string{"go", "typescript"})
	families := &kernel.World{Families: map[string]*model.Family{"probe": loaded.Families["probe"], "carrier": loaded.Families["carrier"]}, Names: []string{"carrier", "probe"}, Problems: map[string][]diag.Diagnostic{}}
	// The left path: probe and the carrier with S bound to probe, at the
	// default layout.
	left, _ := toolKernel(load.Config{}, module, scope, "")
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
