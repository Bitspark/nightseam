package main

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/check"
	"github.com/Bitspark/nightseam/internal/compose"
	"github.com/Bitspark/nightseam/internal/load"
	"github.com/Bitspark/nightseam/internal/model/builtin"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/targets/golang"
)

// proofWorld reads the proof checkout: one contract using every form the
// type language gained at once, beside the family whose side it extends.
func proofWorld(t *testing.T) analysis.World {
	t.Helper()
	world, diagnostics := load.Checkout(os.DirFS("testdata"), "proof", []string{"go", "typescript", "markdown"})
	for _, d := range diagnostics {
		t.Errorf("loading the proof checkout: %s", d)
	}
	if len(world.Names) == 0 {
		t.Fatal("the proof checkout holds no families")
	}
	return analysis.World(world.Families)
}

// TestProofFamilyIsAccepted: the proof contract — a generic union, a union
// that extends another, a variant payload with a literal field and
// one that is not an object at all, a shape written inline, a collection of
// values that may be null, a type parameter beside a family parameter of
// the protocol tier on one declaration, a local application, a side that
// extends another family's, and a pattern in the dialect — is accepted by
// the neutral checks. Both language targets and the specification also
// accept it and hold their complete output in golden corpora.
func TestProofFamilyIsAccepted(t *testing.T) {
	world := proofWorld(t)
	for name := range world {
		for _, d := range check.Family(analysis.Resolve(world, name)) {
			t.Errorf("the proof checkout is refused: %s", d)
		}
	}
}

// TestProofFamilyUsesEveryNewForm holds the settled forms as a fixed list,
// so dropping a form from the proof cannot silently leave it unproved.
func TestProofFamilyUsesEveryNewForm(t *testing.T) {
	r := render.Build(analysis.Resolve(proofWorld(t), "proof"))
	var got []string
	for _, form := range render.FormsUsed(r) {
		got = append(got, form.Name)
	}
	want := []string{
		"a family parameter of the protocol tier",
		"a type parameter of the family",
		"a nullable type expression, {\"nullable\": T}",
		"an application of a generic type of this family",
		"a union",
		"a type parameter of a type",
		"a shape written inline, named by where it sits",
		"a literal type",
		"a side that extends another family's",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the proof family uses:\n%v\nwant:\n%v", got, want)
	}
}

// TestProofFamilyDerivesItsInlineNames: each shape the proof family writes
// inline is generated under the name the one derivation rule gives it, from
// the path to where it sits — the rule conformance/tables/naming.json holds
// and every language's target reads.
func TestProofFamilyDerivesItsInlineNames(t *testing.T) {
	got := map[string]string{}
	for _, inline := range analysis.Resolve(proofWorld(t), "proof").Inlines() {
		got[inline.Name] = inline.At.String()
	}
	want := map[string]string{
		"OptionNone":    "model.json#/types/Option/variants/none",
		"PartImage":     "model.json#/types/Part/variants/image",
		"RichPartTable": "model.json#/types/RichPart/variants/table",
		"PartsRequest":  "protocol.json#/server/methods/parts/request",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the shapes written inline derive:\n%v\nwant:\n%v", got, want)
	}
}

// Every composed target accepts the complete language proof; a new renderer
// must uphold the same contract before joining the composition.
func TestEveryTargetAcceptsSettledLanguage(t *testing.T) {
	r := render.Build(analysis.Resolve(proofWorld(t), "proof"))
	for _, target := range compose.Targets("example.com/api", "@example", "") {
		if diagnostics := target.Check(r); len(diagnostics) != 0 {
			t.Errorf("%s refuses the complete proof: %v", target.Name(), diagnostics)
		}
	}
}

// The Go half holds the new forms and their exported names before the
// cross-language gate admits the proof into the shared corpus.
func TestGoProofRenderingAndSurfaceGolden(t *testing.T) {
	family := render.Build(analysis.Resolve(proofWorld(t), "proof"))
	target := golang.New(golang.Config{Module: module})
	if diagnostics := target.Check(family); len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	files, err := target.Render(family)
	if err != nil {
		t.Fatal(err)
	}
	goldens := map[string][]byte{}
	for _, file := range files {
		goldens[file.Path] = file.Data
		goldens[file.Path+".surface.txt"] = []byte(strings.Join(goSurface(t, file.Path, string(file.Data)), "\n") + "\n")
	}
	holdGolden(t, "testdata/golden-go-proof", goldens)
}

// TestBuiltinFamiliesPassTheirOwnChecks: the families Nightseam declares of
// itself are held to the rules every consumer's family is held to, in a
// world of their own — a built-in that the tool would refuse from a
// consumer would be one rule for us and another for them.
func TestBuiltinFamiliesPassTheirOwnChecks(t *testing.T) {
	world := analysis.World(builtin.Families())
	if len(world) == 0 {
		t.Fatal("there are no built-in families")
	}
	for name := range world {
		for _, d := range check.Family(analysis.Resolve(world, name)) {
			t.Errorf("the built-in %s family is refused: %s", name, d)
		}
	}
}
