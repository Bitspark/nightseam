package runtime

import (
	"encoding/json"
	"strings"
	"testing"
)

// These methods model generated WireType methods, including lazy generic
// arguments. The test supplies their schema before asking TypeArgument to
// resolve them; Bind itself must never call these methods.
var drawnGeneratedSchema *Schema
var drawnGeneratedCalls int

type drawnGeneratedEnvelope struct{}
type drawnGeneratedHandle string
type drawnGeneratedJob struct{}

func (drawnGeneratedEnvelope) WireType() TypeBinding {
	drawnGeneratedCalls++
	return TypeBinding{Schema: drawnGeneratedSchema, Type: "Envelope"}
}
func (drawnGeneratedHandle) WireType() TypeBinding {
	drawnGeneratedCalls++
	return TypeBinding{Schema: drawnGeneratedSchema, Type: "Handle"}
}
func (drawnGeneratedJob) WireType() TypeBinding {
	drawnGeneratedCalls++
	return TypeBinding{Schema: drawnGeneratedSchema, Type: "Job"}
}

func drawnSourceFixture(t *testing.T, name string) *Schema {
	t.Helper()
	family := identityFixtures(t)[name].Families["same"]
	return MustSchema(family.Wire, family.Digest, nil).MustWithDeclaration(family.Declaration)
}

func drawnConsumerFixture(t *testing.T) *Schema {
	t.Helper()
	parameters := []any{map[string]any{"name": "S", "of": "protocol"}}
	definitions := map[string]any{
		"consumer": map[string]any{"kind": "family", "parameters": parameters, "types": map[string]any{}, "errors": []any{}, "server": map[string]any{"ref": "consumer/$server"}, "client": map[string]any{"ref": "consumer/$client"}},
	}
	for _, direction := range []string{"server", "client"} {
		definitions["consumer/$"+direction] = map[string]any{"kind": "side", "direction": direction, "parameters": parameters, "extends": []any{}, "methods": map[string]any{}, "events": map[string]any{}}
	}
	document, err := (declarationGraph{Version: 1, Root: map[string]any{"ref": "consumer"}, Definitions: definitions}).document()
	if err != nil {
		t.Fatal(err)
	}
	return MustSchema(`{"types":{},"parameters":[{"name":"S","of":"protocol"}]}`, declarationHash(document), nil).MustWithDeclaration(document)
}

func TestDrawnFamilyIdentityRecoversGeneratedProvenance(t *testing.T) {
	source := drawnSourceFixture(t, "same named argument first revision")
	drawnGeneratedSchema, drawnGeneratedCalls = source, 0
	t.Cleanup(func() { drawnGeneratedSchema = nil })
	consumer := drawnConsumerFixture(t)
	first := consumer.Bind(map[string]any{"S.Envelope": TypeArgument[drawnGeneratedEnvelope]()}, nil)
	bound := first.Bind(map[string]any{"S.Handle": TypeArgument[drawnGeneratedHandle](), "S.Job": TypeArgument[drawnGeneratedJob]()}, nil)
	if drawnGeneratedCalls != 0 {
		t.Fatal("Bind eagerly resolved generated type metadata")
	}
	got, err := bound.BoundDeclaration()
	if err != nil {
		t.Fatal(err)
	}
	want, err := consumer.Bind(nil, map[string]*Schema{"S": source}).BoundDeclaration()
	if err != nil {
		t.Fatal(err)
	}
	if got != want || !strings.Contains(got, source.declaration) {
		t.Fatalf("drawn family did not retain its complete canonical root\ngot %s\nwant %s", got, want)
	}
	if drawnGeneratedCalls == 0 {
		t.Fatal("identity did not resolve lazy generated provenance")
	}
	if digest, err := bound.DeclarationDigest(); err != nil || digest != declarationHash(want) {
		t.Fatalf("digest=%s, error=%v", digest, err)
	}
	if err := bound.ValidateExpressionRaw("S.Job", []byte(`{"value":"held"}`)); err != nil {
		t.Fatal(err)
	}
	if err := bound.ValidateExpressionRaw("S.Job", []byte(`{"value":42}`)); err == nil {
		t.Fatal("validation lost the member descriptor")
	}
	if err := first.ValidateExpressionRaw("S.Job", []byte(`{"value":"held"}`)); err == nil {
		t.Fatal("a later Bind mutated the prior validation table")
	}
	if _, err := consumer.DeclarationDigest(); err == nil {
		t.Fatal("missing family binding acquired identity")
	}
}

func TestDrawnFamilyIdentityRefusesMixedOrMissingAssociations(t *testing.T) {
	first := drawnSourceFixture(t, "same named argument first revision")
	second := drawnSourceFixture(t, "same named argument second revision")
	// The two revisions have the same family path and carried wire layouts.
	// Even an unrelated reachable declaration change changes family identity.
	foreignDocument := strings.ReplaceAll(first.declaration, "same", "foreign")
	foreign := *first
	foreign.declaration, foreign.digest = foreignDocument, declarationHash(foreignDocument)
	consumer := drawnConsumerFixture(t)
	cases := []struct {
		name     string
		bindings map[string]any
		want     string
	}{
		{"mixed revision", map[string]any{"S.Envelope": TypeBinding{Schema: first, Type: "Envelope"}, "S.Handle": TypeBinding{Schema: second, Type: "Handle"}}, "different family interpretation"},
		{"mixed nominal family", map[string]any{"S.Envelope": TypeBinding{Schema: first, Type: "Envelope"}, "S.Handle": TypeBinding{Schema: &foreign, Type: "Handle"}}, "different family interpretation"},
		{"wrong member", map[string]any{"S.Envelope": TypeBinding{Schema: first, Type: "Envelope"}, "S.Handle": TypeBinding{Schema: first, Type: "Job"}}, "does not match its family declaration"},
		{"unassociated table", map[string]any{"S.Envelope": TypeArgument[string](), "S.Handle": TypeArgument[string]()}, "no canonical family provenance"},
		{"unknown member", map[string]any{"S.Envelope": TypeBinding{Schema: first, Type: "Envelope"}, "S.Missing": TypeArgument[string]()}, "expected known type"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			bound := consumer.Bind(test.bindings, nil)
			if _, err := bound.DeclarationDigest(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("identity error=%v, want %s", err, test.want)
			}
		})
	}
}

func TestDrawnFamilyIdentityRetainsBoundArguments(t *testing.T) {
	source := drawnSourceFixture(t, "same named argument first revision")
	graph, err := readDeclaration(source.declaration)
	if err != nil {
		t.Fatal(err)
	}
	parameters := []any{map[string]any{"name": "T", "of": ""}}
	graph.Definitions["same"].(map[string]any)["parameters"] = parameters
	job := graph.Definitions["same/Job"].(map[string]any)
	job["captures"] = parameters
	job["fields"].([]any)[0].(map[string]any)["type"] = map[string]any{"parameter": "same/T"}
	document, _ := graph.document()
	wire, err := json.Marshal(map[string]any{"types": map[string]any{"Envelope": source.types["Envelope"], "Handle": source.types["Handle"], "Job": map[string]any{"kind": "record", "fields": []any{map[string]any{"name": "value", "type": "T", "required": true}}}}, "parameters": []any{map[string]any{"name": "T"}}})
	if err != nil {
		t.Fatal(err)
	}
	template := MustSchema(string(wire), declarationHash(document), nil).MustWithDeclaration(document)
	text := template.Bind(map[string]any{"T": TypeArgument[string]()}, nil)
	integer := template.Bind(map[string]any{"T": TypeArgument[int64]()}, nil)
	consumer := drawnConsumerFixture(t)
	bound := consumer.Bind(map[string]any{"S.Envelope": TypeBinding{Schema: text, Type: "Envelope"}, "S.Handle": TypeBinding{Schema: text, Type: "Handle"}, "S.Job": TypeBinding{Schema: text, Type: "Job"}}, nil)
	got, err := bound.BoundDeclaration()
	if err != nil {
		t.Fatal(err)
	}
	want, err := consumer.Bind(nil, map[string]*Schema{"S": text}).BoundDeclaration()
	if err != nil || got != want {
		t.Fatalf("bound arguments changed: %v\n%s\n%s", err, got, want)
	}
	if err := bound.ValidateExpressionRaw("S.Job", []byte(`{"value":"held"}`)); err != nil {
		t.Fatal(err)
	}
	if err := bound.ValidateExpressionRaw("S.Job", []byte(`{"value":42}`)); err == nil {
		t.Fatal("bound validation lost T")
	}
	mixed := consumer.Bind(map[string]any{"S.Envelope": TypeBinding{Schema: text, Type: "Envelope"}, "S.Handle": TypeBinding{Schema: integer, Type: "Handle"}}, nil)
	if _, err := mixed.DeclarationDigest(); err == nil || !strings.Contains(err.Error(), "different family interpretation") {
		t.Fatalf("mixed application error: %v", err)
	}
	missing := consumer.Bind(map[string]any{"S.Envelope": TypeBinding{Schema: template, Type: "Envelope"}, "S.Handle": TypeBinding{Schema: template, Type: "Handle"}}, nil)
	if _, err := missing.DeclarationDigest(); err == nil || !strings.Contains(err.Error(), "missing required binding T") {
		t.Fatalf("missing argument error: %v", err)
	}
}
