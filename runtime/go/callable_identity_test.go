package runtime

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func customCallableFixtures(t *testing.T) map[string]*Schema {
	t.Helper()
	data, err := os.ReadFile("../../conformance/tables/callable-identities.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Families map[string]struct {
			Wire        any
			Declaration any
			Imports     []string
		}
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	result := map[string]*Schema{}
	for name, fixture := range table.Families {
		wire, _ := json.Marshal(fixture.Wire)
		declaration, _ := json.Marshal(fixture.Declaration)
		result[name] = MustSchema(string(wire), declarationHash(string(declaration)), nil).MustWithDeclaration(string(declaration))
	}
	for name, fixture := range table.Families {
		for _, imported := range fixture.Imports {
			result[name].imported[imported] = result[imported]
		}
	}
	return result
}

func TestCallableIdentityCapturedScopesAndAliases(t *testing.T) {
	schemas := customCallableFixtures(t)
	captured, consumer := schemas["captured"], schemas["consumer"]
	integer := TypeBinding{Schema: captured, Type: "integer"}
	text := TypeBinding{Schema: captured, Type: "string"}
	bound := TypeBinding{Schema: captured.Bind(map[string]any{"T": text}, nil), Type: "Callback"}
	identity, err := CallableIdentity(bound)
	if err != nil || identity.Path != "captured/Callback<string>" {
		t.Fatalf("capture: %v %v", identity, err)
	}
	for _, binding := range []TypeBinding{{Schema: consumer, Type: "Closed"}, {Schema: consumer, Type: MustTypeExpression(`{"apply":"captured.Callback","with":{"T":"string"}}`)}} {
		got, err := CallableIdentity(binding)
		if err != nil || got != identity {
			t.Fatalf("imported capture/alias differs: %v %v", got, err)
		}
		if err := validateCallableReference(binding, map[string]any{"binding": "opaque", "contract": identity.Path, "digest": identity.Digest}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := CallableIdentity(TypeBinding{Schema: captured, Type: "Callback"}); err == nil {
		t.Fatal("unbound capture accepted")
	}
	if err := validateCallableReference(TypeBinding{Schema: captured, Type: "Callback"}, map[string]any{"binding": "opaque", "contract": "captured/Callback"}); err == nil {
		t.Fatal("unbound capture accepted by validator")
	}
	plain, err := CallableIdentity(TypeBinding{Schema: consumer, Type: "Alias"})
	if err != nil || plain.Path != "captured/Plain" || plain.Digest != captured.digest || plain.Digest == consumer.digest {
		t.Fatalf("nongeneric alias: %v %v", plain, err)
	}
	other, err := CallableIdentity(TypeBinding{Schema: captured.Bind(map[string]any{"T": integer}, nil), Type: "Callback"})
	if err != nil || other.Path == identity.Path || other.Digest == identity.Digest {
		t.Fatalf("capture collapsed: %v %v", other, err)
	}
	if err := validateCallableReference(bound, map[string]any{"binding": "opaque", "contract": other.Path}); err == nil {
		t.Fatal("wrong capture accepted without digest")
	}
	entityKey := TypeBinding{Schema: captured, Type: MustTypeExpression(`{"ref":"Entity"}`)}
	entity, err := CallableIdentity(TypeBinding{Schema: captured.Bind(map[string]any{"T": entityKey}, nil), Type: "Callback"})
	if err != nil || entity.Path != "captured/Callback<&captured/Entity>" {
		t.Fatalf("entity-key argument: %v %v", entity, err)
	}
	drawn := TypeBinding{Schema: schemas["drawn"].Bind(nil, map[string]*Schema{"S": bound.Schema}), Type: "Callback"}
	drawIdentity, err := CallableIdentity(drawn)
	if err != nil || drawIdentity.Path != "drawn/Callback<captured<string>>" {
		t.Fatalf("bound family capture: %v %v", drawIdentity, err)
	}
	if err := validateCallableReference(drawn, map[string]any{"binding": "opaque", "contract": drawIdentity.Path, "digest": drawIdentity.Digest}); err != nil {
		t.Fatal(err)
	}
}

func TestCallableIdentityRevisionRefusalBeforeInterpretation(t *testing.T) {
	rows := identityFixtures(t)
	first := callableFixtureBinding(t, rows, "application argument declaration baseline", nil, nil)
	second := callableFixtureBinding(t, rows, "application reachable argument changed", nil, nil)
	identity, err := CallableIdentity(first)
	if err != nil {
		t.Fatal(err)
	}
	err = validateCallableReference(second, map[string]any{"binding": "opaque", "contract": identity.Path, "digest": identity.Digest})
	var public *PublicError
	if !errors.As(err, &public) || public.Code != "contract_mismatch" {
		t.Fatalf("revision refusal: %v", err)
	}
	if err := validateCallableReference(second, map[string]any{"binding": "opaque", "contract": identity.Path}); err != nil {
		t.Fatalf("unspecified revision: %v", err)
	}
	if _, err := CallableIdentity(TypeBinding{Schema: MustSchema(`{"types":{"Plain":{"kind":"callable","contract":"x/Plain"}}}`, "", nil), Type: "Plain"}); err == nil {
		t.Fatal("public identity accepted absent provenance")
	}
}

type callableIdentitySlot struct {
	Source string                          `json:"source"`
	Type   json.RawMessage                 `json:"type"`
	Slots  map[string]callableIdentitySlot `json:"slots"`
}

type callableIdentityCase struct {
	ID, Source, Path, Digest, Error string
	Slots                           map[string]callableIdentitySlot
}

func validateCallableReference(binding TypeBinding, reference map[string]any) error {
	raw, err := json.Marshal(reference)
	if err != nil {
		return err
	}
	return binding.Schema.ValidateExpressionRaw(binding.Type, raw)
}

func callableFixtureBinding(t *testing.T, rows map[string]identityFixture, source string, typ json.RawMessage, slots map[string]callableIdentitySlot) TypeBinding {
	t.Helper()
	row := rows[source]
	if row.Name == "" {
		t.Fatalf("missing declaration fixture %s", source)
	}
	schemas := map[string]*Schema{}
	for name, family := range row.Families {
		schemas[name] = MustSchema(family.Wire, family.Digest, nil).MustWithDeclaration(family.Declaration)
	}
	for name, family := range row.Families {
		for _, imported := range family.Imports {
			schemas[name].imported[imported] = schemas[imported]
		}
	}
	bound := map[string]any{}
	for name, slot := range slots {
		origin := slot.Source
		if origin == "" {
			origin = source
		}
		bound[name] = callableFixtureBinding(t, rows, origin, slot.Type, slot.Slots)
	}
	if len(typ) == 0 {
		typ = row.WireExpression
	}
	return TypeBinding{Schema: schemas[row.Family].Bind(bound, nil), Type: MustTypeExpression(string(typ))}
}

func TestCallableIdentitySharedPaths(t *testing.T) {
	data, err := os.ReadFile("../../conformance/tables/callable-identities.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct{ Cases []callableIdentityCase }
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	rows := identityFixtures(t)
	for _, row := range table.Cases {
		t.Run(row.ID, func(t *testing.T) {
			binding := callableFixtureBinding(t, rows, row.Source, nil, row.Slots)
			got, err := CallableIdentity(binding)
			if row.Error != "" {
				if err == nil || !strings.Contains(err.Error(), row.Error) {
					t.Fatalf("got %v, want %s", err, row.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Path != row.Path {
				t.Fatalf("path\ngot  %s\nwant %s", got.Path, row.Path)
			}
			if row.Digest != "" && got.Digest != row.Digest {
				t.Fatalf("digest got %s, want %s", got.Digest, row.Digest)
			}
			declaration, err := binding.Declaration()
			if err != nil || got.Digest != declarationHash(declaration) {
				t.Fatalf("applied digest does not identify selected graph: %v", err)
			}
			reference := map[string]any{"binding": "opaque", "contract": got.Path, "digest": got.Digest}
			if err := validateCallableReference(binding, reference); err != nil {
				t.Fatalf("own reference: %v", err)
			}
			delete(reference, "digest")
			if err := validateCallableReference(binding, reference); err != nil {
				t.Fatalf("absent revision: %v", err)
			}
			reference["contract"] = "same/Function"
			if err := validateCallableReference(binding, reference); err == nil {
				t.Fatal("template contract bypassed applied nominal check without digest")
			}
		})
	}
}
