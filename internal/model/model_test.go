package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestTypeExprRoundTrips: every form of a type expression decodes to its
// variant and marshals back to the bytes it came from.
func TestTypeExprRoundTrips(t *testing.T) {
	for source, want := range map[string]TypeExpr{
		`"string"`:                    Primitive("string"),
		`"json"`:                      Primitive("json"),
		`"Project"`:                   Named{Name: "Project"},
		`"identity.User"`:             Imported{Family: "identity", Name: "User"},
		`"my-family.User"`:            Imported{Family: "my-family", Name: "User"},
		`"S.Envelope"`:                Drawn{Parameter: "S", Name: "Envelope"},
		`"S.Payload"`:                 Drawn{Parameter: "S", Name: "Payload"},
		`{"array":"string"}`:          Array{Elem: Primitive("string")},
		`{"map":{"array":"Project"}}`: Map{Elem: Array{Elem: Named{Name: "Project"}}},
		`{"ref":"Project"}`:           Ref{Entity: "Project"},
		`{"apply":"carrier.Frame","with":{"S":"B"}}`:     Apply{Family: "carrier", Name: "Frame", With: map[string]Filler{"S": {Parameter: "B"}}},
		`{"apply":"carrier.Frame","with":{"S":"probe"}}`: Apply{Family: "carrier", Name: "Frame", With: map[string]Filler{"S": {Family: "probe"}}},
	} {
		got, err := Decode(json.RawMessage(source))
		if err != nil {
			t.Errorf("%s: %v", source, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s decoded to %#v, not %#v", source, got, want)
		}
		if back := String(got); back != source {
			t.Errorf("%s marshals back as %s", source, back)
		}
	}
}

// TestTypeExprRefuses: what is not one form is refused with a reason.
func TestTypeExprRefuses(t *testing.T) {
	for source, reason := range map[string]string{
		``:                                    "required",
		`null`:                                "required",
		`""`:                                  "required",
		`42`:                                  "string or an object",
		`{}`:                                  "one of array, map, ref",
		`{"array":"A","map":"B"}`:             "one of array, map, ref",
		`{"envelope":"S"}`:                    "one of array, map, ref",
		`{"apply":"carrier.Frame"}`:           "apply needs with",
		`{"apply":"Frame","with":{}}`:         "family.Type",
		`{"apply":"S.Frame","with":{}}`:       "family.Type",
		`{"apply":"c.Frame","with":{"S":""}}`: "filled by nothing",
		`{"ref":"other.User"}`:                "entity of this family",
		`"a.b.c"`:                             "family.Type or Param.Type",
		`".User"`:                             "family.Type or Param.Type",
		`"probe."`:                            "family.Type or Param.Type",
	} {
		_, err := Decode(json.RawMessage(source))
		if err == nil {
			t.Errorf("%s was accepted", source)
			continue
		}
		if !strings.Contains(err.Error(), reason) {
			t.Errorf("%s: %v does not say %q", source, err, reason)
		}
	}
}

// TestReferenceFormByCase: a qualified name is a parameter's type when the
// qualifier is upper camel case and an imported family's when lower, and
// what fills an application is told apart the same way.
func TestReferenceFormByCase(t *testing.T) {
	if !IsParameter("S") || !IsParameter("Session") || IsParameter("probe") || IsParameter("") {
		t.Fatal("IsParameter tells the cases apart wrong")
	}
	if FillerOf("B") != (Filler{Parameter: "B"}) || FillerOf("probe") != (Filler{Family: "probe"}) {
		t.Fatal("FillerOf tells the cases apart wrong")
	}
	if FillerOf("B").String() != "B" || FillerOf("probe").String() != "probe" {
		t.Fatal("a filler spells itself as it was written")
	}
}

// TestWalkAndRewrite: Walk visits parents first and stops where told;
// Rewrite rebuilds from the leaves so that a parameter's slot can be filled
// inside any nesting.
func TestWalkAndRewrite(t *testing.T) {
	expr := MustDecode(`{"array":{"map":"S.Envelope"}}`)
	var seen []string
	Walk(expr, func(e TypeExpr) bool {
		seen = append(seen, String(e))
		_, isMap := e.(Map)
		return !isMap
	})
	if want := []string{`{"array":{"map":"S.Envelope"}}`, `{"map":"S.Envelope"}`}; !reflect.DeepEqual(seen, want) {
		t.Fatalf("Walk visited %v", seen)
	}
	bound := Rewrite(expr, func(e TypeExpr) TypeExpr {
		if d, ok := e.(Drawn); ok && d.Parameter == "S" {
			return Imported{Family: "probe", Name: d.Name}
		}
		return e
	})
	if got := String(bound); got != `{"array":{"map":"probe.Envelope"}}` {
		t.Fatalf("Rewrite gave %s", got)
	}
	if !Equal(expr, MustDecode(`{"array":{"map":"S.Envelope"}}`)) || Equal(expr, bound) {
		t.Fatal("Equal compares the expressions")
	}
}

// TestDecodeTypes: a types section decodes with every field located, a
// field required unless it says otherwise, an alias's target decoded.
func TestDecodeTypes(t *testing.T) {
	types, err := DecodeTypes("model.json", json.RawMessage(`{
		"User": {"kind": "entity", "key": "id", "description": "A person.", "fields": [
			{"name": "id", "type": "string"},
			{"name": "email", "type": "string", "unique": true, "pattern": "@"},
			{"name": "name", "type": "string", "required": false, "nullable": true, "length": {"max": 120}}
		]},
		"Page": {"kind": "record", "extends": ["Base"], "open": true, "fields": [{"name": "offset", "type": "integer", "min": 0}]},
		"Status": {"kind": "enum", "values": ["a", "b"]},
		"Ids": {"kind": "alias", "type": {"array": {"ref": "User"}}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	user := types["User"]
	if user.Kind != KindEntity || user.Key != "id" || user.Description != "A person." || len(user.Fields) != 3 {
		t.Fatalf("User decoded as %+v", user)
	}
	if user.At.String() != "model.json#/types/User" || user.Fields[2].At.String() != "model.json#/types/User/fields/2" {
		t.Fatalf("locations are %s and %s", user.At, user.Fields[2].At)
	}
	if !user.Fields[0].Required || user.Fields[2].Required || !user.Fields[2].Nullable || !user.Fields[1].Unique || user.Fields[1].Pattern != "@" {
		t.Fatal("field flags decoded wrong")
	}
	if user.Fields[2].Length == nil || user.Fields[2].Length.Max == nil || *user.Fields[2].Length.Max != 120 {
		t.Fatal("length decoded wrong")
	}
	page := types["Page"]
	if !page.Open || page.Extends[0] != "Base" || page.Fields[0].Min == nil || page.Fields[0].Min.String() != "0" {
		t.Fatalf("Page decoded as %+v", page)
	}
	if types["Status"].Values[1] != "b" || String(types["Ids"].Alias) != `{"array":{"ref":"User"}}` {
		t.Fatal("enum or alias decoded wrong")
	}
	if _, err := DecodeTypes("model.json", json.RawMessage(`{"X": {"kind": "record", "fields": [{"name": "a", "type": {}}]}}`)); err == nil || !strings.Contains(err.Error(), "model.json#/types/X/fields/0/type") {
		t.Fatalf("a bad field type is located: %v", err)
	}
}

// TestDecodeProtocol: sides decode from maps into slices by name, a method
// without a request has none, errors sort by code, and everything is found
// by name afterwards.
func TestDecodeProtocol(t *testing.T) {
	p, err := DecodeProtocol("protocol.json", json.RawMessage(`{
		"profile": "nightseam.duplex/1",
		"parameters": [{"name": "S", "of": "session", "description": "carried"}],
		"server": {
			"methods": {
				"echo": {"request": "Payload", "result": "Payload", "errors": ["denied"], "description": "Echoes."},
				"no_args": {"result": "string"}
			},
			"events": {"changed": {"type": "Payload"}},
			"crud": {"Project": ["list"]}
		},
		"client": {"methods": {"reverse": {"request": "Payload", "result": "Payload"}}},
		"errors": {"not_found": "No such probe.", "denied": "The caller is denied."}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Profile != "nightseam.duplex/1" || p.Parameters[0].Name != "S" || p.Parameters[0].At.String() != "protocol.json#/parameters/0" {
		t.Fatalf("header decoded as %+v", p)
	}
	if names := []string{p.Server.Methods[0].Name, p.Server.Methods[1].Name}; names[0] != "echo" || names[1] != "no_args" {
		t.Fatalf("server methods are %v", names)
	}
	echo, server, ok := p.Method("echo")
	if !ok || !server || echo.Errors[0] != "denied" || echo.Description != "Echoes." || echo.At.String() != "protocol.json#/server/methods/echo" {
		t.Fatalf("echo is %+v", echo)
	}
	if noArgs, _, _ := p.Method("no_args"); noArgs.Request != nil || String(noArgs.Result) != `"string"` {
		t.Fatal("no_args has a request or the wrong result")
	}
	if reverse, server, ok := p.Method("reverse"); !ok || server || reverse.At.String() != "protocol.json#/client/methods/reverse" {
		t.Fatal("reverse is not on the client side")
	}
	if _, _, ok := p.Method("missing"); ok {
		t.Fatal("a missing method was found")
	}
	if changed, server, ok := p.Event("changed"); !ok || !server || String(changed.Type) != `"Payload"` {
		t.Fatal("changed is wrong")
	}
	if p.Errors[0].Code != "denied" || p.Errors[1].Code != "not_found" || p.Errors[1].At.String() != "protocol.json#/errors/not_found" {
		t.Fatalf("errors are %+v", p.Errors)
	}
	if e, ok := p.Error("denied"); !ok || e.Description != "The caller is denied." {
		t.Fatal("denied is not found")
	}
	if p.Server.CRUD["Project"][0] != "list" {
		t.Fatal("crud is not carried")
	}
	if names := p.ParameterNames(); len(names) != 1 || names[0] != "S" {
		t.Fatal("parameter names are wrong")
	}
}

// TestDecodeSessionAndOverrides: the session's sections decode, extensions
// are carried raw, and an override file refuses a key it does not know.
func TestDecodeSessionAndOverrides(t *testing.T) {
	s, err := DecodeSession("session.json", json.RawMessage(`{"decides": ["echo"], "asks": ["reverse"], "conversation": {"event": "changed", "path": "text"}, "extensions": {"options": [1]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Decides[0] != "echo" || s.Asks[0] != "reverse" || s.Conversation.Event != "changed" || s.Conversation.Path != "text" || string(s.Extensions) != `{"options": [1]}` {
		t.Fatalf("session decoded as %+v", s)
	}
	o, err := DecodeOverrides("go.json", json.RawMessage(`{"names": {"User.id": "ID"}}`))
	if err != nil || o.Names["User.id"] != "ID" {
		t.Fatalf("overrides decoded as %+v, %v", o, err)
	}
	if _, err := DecodeOverrides("go.json", json.RawMessage(`{"types": {}}`)); err == nil {
		t.Fatal("an override file that declares was accepted")
	}
}

// TestExpressions: every expression site of a family is visited once with
// its owner and location, nested expressions included.
func TestExpressions(t *testing.T) {
	types, err := DecodeTypes("protocol.json", json.RawMessage(`{
		"Frame": {"kind": "record", "fields": [{"name": "message", "type": "S.Envelope"}]},
		"Frames": {"kind": "alias", "type": {"array": "Frame"}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	p, err := DecodeProtocol("protocol.json", json.RawMessage(`{"server": {"methods": {"relay": {"request": "Frame", "result": "probe.Envelope"}}, "events": {"seen": {"type": "Frames"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	f := &Family{Name: "carrier", Types: types, Protocol: p}
	var sites []string
	f.Expressions(func(site ExprAt) {
		sites = append(sites, site.Owner+" "+site.At.Pointer+" "+String(site.Expr))
	})
	want := []string{
		`Frame /types/Frame/fields/0/type "S.Envelope"`,
		`Frames /types/Frames/type {"array":"Frame"}`,
		`Frames /types/Frames/type "Frame"`,
		` /server/methods/relay/request "Frame"`,
		` /server/methods/relay/result "probe.Envelope"`,
		` /server/events/seen/type "Frames"`,
	}
	if !reflect.DeepEqual(sites, want) {
		t.Fatalf("visited:\n%s", strings.Join(sites, "\n"))
	}
}
