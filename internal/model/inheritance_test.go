package model

import (
	"encoding/json"
	"testing"
)

func TestInheritanceKeepsExplicitBindingsAndTheirLocations(t *testing.T) {
	types, err := DecodeTypes("model.json", json.RawMessage(`{"Derived":{"kind":"record","extends":["Plain",{"apply":"base.Box","with":{"T":{"array":{"nullable":"Item"}},"F":"source"}}],"fields":[]}}`))
	if err != nil {
		t.Fatal(err)
	}
	edges := types["Derived"].Extends
	if edges[0].Name != "Plain" || edges[0].With != nil || edges[1].Name != "base.Box" || edges[1].With["F"].Family != "source" {
		t.Fatalf("edges = %#v", edges)
	}
	if edges[1].At.String() != "model.json#/types/Derived/extends/1" || String(edges[1].With["T"].Type) != `{"array":{"nullable":"Item"}}` {
		t.Fatalf("applied edge = %#v", edges[1])
	}
	data, err := json.Marshal(types["Derived"])
	if err != nil {
		t.Fatal(err)
	}
	var descriptor struct{ Extends []json.RawMessage }
	if err := json.Unmarshal(data, &descriptor); err != nil {
		t.Fatal(err)
	}
	if string(descriptor.Extends[0]) != `"Plain"` || string(descriptor.Extends[1]) != `{"apply":"base.Box","with":{"F":"source","T":{"array":{"nullable":"Item"}}}}` {
		t.Fatalf("wire edges = %s", data)
	}
	p, err := DecodeProtocol("protocol.json", json.RawMessage(`{"profile":"nightseam.duplex/1","server":{"extends":[{"apply":"base","with":{"T":"string"}}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Server.Extends[0].Name != "base" || p.Server.Extends[0].At.String() != "protocol.json#/server/extends/0" {
		t.Fatalf("side inheritance = %#v", p.Server.Extends)
	}
}

func TestUnionEmptyArmIsDistinctFromAnEmptyRecordAndNullPayload(t *testing.T) {
	types, err := DecodeTypes("model.json", json.RawMessage(`{"Option":{"kind":"union","tag":"kind","variants":{"none":{"empty":true},"record":{"kind":"record","fields":[]},"nullable":{"nullable":"string"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	none, _ := types["Option"].Variant("none")
	object, _ := types["Option"].Variant("record")
	nullable, _ := types["Option"].Variant("nullable")
	if none.Type != nil || object.Type == nil || nullable.Type == nil {
		t.Fatal("only the no-payload arm has no expression")
	}
	data, err := json.Marshal(types["Option"])
	if err != nil {
		t.Fatal(err)
	}
	var wire struct{ Variants map[string]json.RawMessage }
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if string(wire.Variants["none"]) != `{"empty":true}` {
		t.Fatalf("wire = %s", data)
	}
	for _, invalid := range []string{`{"empty":false}`, `{"empty":true,"type":"string"}`, `null`} {
		if _, err := DecodeTypes("model.json", json.RawMessage(`{"Option":{"kind":"union","tag":"kind","variants":{"none":`+invalid+`}}}`)); err == nil {
			t.Errorf("accepted malformed no-payload arm %s", invalid)
		}
	}
	if _, err := Decode(json.RawMessage(`{"empty":true}`)); err == nil {
		t.Fatal("the empty-arm marker is not a type expression")
	}
}
