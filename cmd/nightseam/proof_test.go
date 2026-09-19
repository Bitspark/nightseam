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
)

// proofWorld reads the proof checkout: one contract using every form the
// type language gained at once, beside the family whose side it extends.
func proofWorld(t *testing.T) analysis.World {
	t.Helper()
	world, diagnostics := load.Checkout(os.DirFS("testdata"), "proof", []string{"go", "typescript", "spec"})
	for _, d := range diagnostics {
		t.Errorf("loading the proof checkout: %s", d)
	}
	if len(world.Names) == 0 {
		t.Fatal("the proof checkout holds no families")
	}
	return analysis.World(world.Families)
}

// TestProofFamilyIsAccepted: the proof contract — a generic union, a union
// that extends another, a variant that carries its own tag as a literal and
// one that is not an object at all, a shape written inline, a collection of
// values that may be null, a type parameter beside a family parameter of
// the protocol tier on one declaration, a local application, a side that
// extends another family's, and a pattern in the dialect — is accepted by
// the neutral checks. No target renders it yet, so it is held here by check
// alone until the render lanes land and the gate moves it into the golden
// corpus.
func TestProofFamilyIsAccepted(t *testing.T) {
	world := proofWorld(t)
	for name := range world {
		for _, d := range check.Family(analysis.Resolve(world, name)) {
			t.Errorf("the proof checkout is refused: %s", d)
		}
	}
}

// TestProofFamilyUsesEveryNewForm: every form the targets do not render yet
// is used by the proof family, each named once — so that a form dropped
// from the fixture fails here rather than going unproved, and so that the
// names a target's refusal carries are written down in one place.
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

// TestEveryTargetRefusesWhatItDoesNotRender: every composed target refuses
// the proof family, once per form it does not render yet, naming the form
// and itself. That refusal is what keeps the corpus green while the render
// lanes catch up — a target that met a form it had never been taught would
// otherwise emit something that is not what was declared, or nothing at
// all, and the golden files would say neither.
func TestEveryTargetRefusesWhatItDoesNotRender(t *testing.T) {
	r := render.Build(analysis.Resolve(proofWorld(t), "proof"))
	forms := len(render.FormsUsed(r))
	for _, target := range compose.Targets("example.com/api", "@example", "") {
		diagnostics := target.Check(r)
		if len(diagnostics) != forms {
			t.Errorf("%s refused %d of the %d forms it does not render", target.Name(), len(diagnostics), forms)
		}
		for _, d := range diagnostics {
			if d.Code != "unrendered_form" || !strings.Contains(d.Message, target.Name()) {
				t.Errorf("%s said %s", target.Name(), d)
			}
		}
	}
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
