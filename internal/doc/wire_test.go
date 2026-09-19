package doc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/render"
)

// proof documents the proof family of cmd/nightseam/testdata/families: one
// contract using every form of the settled type language at once, which
// the language targets render and the document describes with example values.
func proof(t *testing.T) *Family {
	t.Helper()
	root := filepath.Join("..", "..", "cmd", "nightseam", "testdata", "families", "api", "contracts")
	families := map[string]map[string]string{}
	for _, name := range []string{"probe", "proof"} {
		entries, err := os.ReadDir(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		families[name] = map[string]string{}
		for _, entry := range entries {
			data, err := os.ReadFile(filepath.Join(root, name, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			families[name][entry.Name()] = string(data)
		}
	}
	return Build(render.Build(analysis.Resolve(analysis.World(modeltest.World(families)), "proof")), nil)
}

func typed(f *Family, name string) *Type {
	for _, t := range f.Types {
		if t.Name == name {
			return t
		}
	}
	return nil
}

// TestExamplesOfEveryForm: an example of each form of the type language is
// what the wire carries — a union at its first variant, inherited variants
// first and by tag among them, its complete payload under the value member
// whatever it is, an empty record included; a literal as its value, a
// nullable as a value, a shape written inline as its fields, a generic
// type as its filler fills it, a draw through a parameter and a type
// parameter with visible concrete bindings, a length as padding, an enum as its first
// value — deterministically.
func TestExamplesOfEveryForm(t *testing.T) {
	f := proof(t)
	for name, want := range map[string]string{
		"Part":     `{"type":"count","value":0}`,
		"RichPart": `{"type":"count","value":0}`,
		"TextPart": `{"type":"text","body":"‹body›"}`,
		"Option":   `{"kind":"none","value":{}}`,
		"Result":   `{"kind":"err","value":"‹err›"}`,
		"Page":     `{"items":["‹items›"],"next":"‹next›"}`,
		"Parts":    `{"items":[{"type":"count","value":0}],"next":"‹next›"}`,
	} {
		tt := typed(f, name)
		if tt == nil {
			t.Fatalf("no type %s", name)
		}
		if got := string(tt.Example); got != want {
			t.Errorf("%s: %s\n   want %s", name, got, want)
		}
	}
	again := proof(t)
	for i := range f.Types {
		if string(f.Types[i].Example) != string(again.Types[i].Example) {
			t.Errorf("%s: the example is not deterministic", f.Types[i].Name)
		}
	}
}

// worker documents the worker family of cmd/nightseam/testdata/families,
// whose live tier declares callables: what a document says a live value looks
// like where one is carried.
func worker(t *testing.T) *Family {
	t.Helper()
	root := filepath.Join("..", "..", "cmd", "nightseam", "testdata", "families", "api", "contracts")
	families := map[string]map[string]string{}
	for _, name := range []string{"worker"} {
		entries, err := os.ReadDir(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		families[name] = map[string]string{}
		for _, entry := range entries {
			data, err := os.ReadFile(filepath.Join(root, name, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			families[name][entry.Name()] = string(data)
		}
	}
	return Build(render.Build(analysis.Resolve(analysis.World(modeltest.World(families)), "worker")), nil)
}

// TestACallablesExampleIsTheReferenceThatNamesIt: a live value on the wire is
// a reference to a binding, so that is what the document shows — of the
// callable itself, of a record holding one, and inside a container and a sum.
// The example a document emits is a value both validators accept; null is not
// one here, and was what a kind the synthesizer did not know fell through to.
//
// It is what the value looks like crossing the seam, not what a consumer
// writes: a consumer writes a function and the generated codec exports it.
func TestACallablesExampleIsTheReferenceThatNamesIt(t *testing.T) {
	f := worker(t)
	for name, want := range map[string]string{
		"Cancel":       `{"binding":"‹binding›","contract":"worker/Cancel"}`,
		"Report":       `{"binding":"‹binding›","contract":"worker/Report"}`,
		"ProgressSink": `{"report":{"binding":"‹binding›","contract":"worker/Report"}}`,
		"Watchers":     `[{"binding":"‹binding›","contract":"worker/Report"}]`,
		"Sinks":        `{"‹key›":{"report":{"binding":"‹binding›","contract":"worker/Report"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			declared := typed(f, name)
			if declared == nil {
				t.Fatalf("the worker family declares no %s", name)
			}
			if got := string(declared.Example); got != want {
				t.Errorf("example of %s is %s; want %s", name, got, want)
			}
		})
	}
	// A data type of the same family is untouched: the reference form reaches
	// exactly the positions that carry a callable.
	if got := string(typed(f, "Ticket").Example); !strings.Contains(got, "‹id›") || strings.Contains(got, "binding") {
		t.Errorf("a data type's example became a reference: %s", got)
	}
}

// Every variant has its own complete envelope, including inherited arms.
// An empty record remains a payload, and recursive arms fold at the same
// declaration boundary as the type's primary example.
func TestEveryUnionVariantHasItsOwnExample(t *testing.T) {
	f := proof(t)
	for _, name := range []string{"Part", "RichPart", "Option", "Result"} {
		typ := typed(f, name)
		for _, variant := range typ.Variants {
			var value map[string]json.RawMessage
			if err := json.Unmarshal(variant.Example, &value); err != nil {
				t.Fatalf("%s.%s: %v", name, variant.Tag, err)
			}
			var tag string
			if err := json.Unmarshal(value[typ.Tag], &tag); err != nil || tag != variant.Tag {
				t.Fatalf("%s.%s: wrong tag in %s", name, variant.Tag, variant.Example)
			}
			if _, present := value[typ.Value]; present == variant.Empty {
				t.Fatalf("%s.%s: wrong payload presence in %s", name, variant.Tag, variant.Example)
			}
		}
	}
	w := analysis.World(modeltest.World(map[string]map[string]string{"x": {"model.json": `{"nightseam":2,"types":{"Choice":{"kind":"union","tag":"kind","value":"body","variants":{"none":{"empty":true},"record":{"kind":"record","fields":[]},"again":"Choice"}}}}`}}))
	typ := typed(Build(render.Build(analysis.Resolve(w, "x")), nil), "Choice")
	want := map[string]string{"none": `{"kind":"none"}`, "record": `{"kind":"record","body":{}}`}
	for _, variant := range typ.Variants {
		if variant.Tag == "again" {
			if variant.Example != nil || variant.ExampleUnavailable == nil {
				t.Fatal("recursive arm invented an invalid null")
			}
			continue
		}
		if got := string(variant.Example); got != want[variant.Tag] {
			t.Errorf("%s: %s, want %s", variant.Tag, got, want[variant.Tag])
		}
	}
}

// TestFramesAreTheProfiles: a method's exchange is the request and the
// response of the profile with the side's ids — c:1 for a method of the
// server, which the client calls — params an object and result an example
// of the declared result, an application of a generic union included; an
// event is its frame with its data; and the weight of each is what its
// example carries.
func TestFramesAreTheProfiles(t *testing.T) {
	f := proof(t)
	var parts, relay *Method
	for i := range f.Server.Methods {
		switch f.Server.Methods[i].Name {
		case "parts":
			parts = &f.Server.Methods[i]
		case "relay":
			relay = &f.Server.Methods[i]
		}
	}
	if parts == nil || relay == nil {
		t.Fatal("the proof family's methods are missing")
	}
	if got := string(parts.Frames.Request); got != `{"version":1,"kind":"request","id":"c:1","method":"parts","params":{"after":"‹after›"}}` {
		t.Errorf("parts request: %s", got)
	}
	if got := string(parts.Frames.Response); got != `{"version":1,"kind":"response","id":"c:1","result":{"kind":"err","value":"‹err›"}}` {
		t.Errorf("parts response: %s", got)
	}
	if parts.Weight != (Weight{Request: 1, Result: 2}) {
		t.Errorf("parts weighs %+v", parts.Weight)
	}
	if got := string(relay.Frames.Response); got != `{"version":1,"kind":"response","id":"c:1","result":{"kind":"none","value":{}}}` {
		t.Errorf("relay response: %s", got)
	}
	// The server side extends probe's, so probe's event is here beside the
	// family's own, inherited.
	var added *Event
	for i := range f.Server.Events {
		if f.Server.Events[i].Name == "part.added" {
			added = &f.Server.Events[i]
		}
	}
	if len(f.Server.Events) != 2 || f.Server.Events[0].Name != "changed" || f.Server.Events[0].Origin.Family != "probe" {
		t.Errorf("the inherited event is not here first: %+v", f.Server.Events)
	}
	if added == nil || string(added.Frame) != `{"version":1,"kind":"event","event":"part.added","data":{"type":"count","value":0}}` || added.Weight != 2 {
		t.Errorf("the event is %+v", added)
	}
	for _, m := range append(f.Server.Methods, f.Client.Methods...) {
		for _, frame := range append([]json.RawMessage{m.Frames.Request, m.Frames.Response}, refusals(m)...) {
			if !json.Valid(frame) {
				t.Errorf("%s: %s is not JSON", m.Name, frame)
			}
		}
	}
}

func refusals(m Method) []json.RawMessage {
	var out []json.RawMessage
	for _, r := range m.Frames.Refusals {
		out = append(out, r.Frame)
	}
	return out
}

// TestRefusalsAndReferences: a method that declares errors has a refusal
// per error carrying the error's description as its message, or its code
// where it has none; a method of the client is called by the server, s:1;
// and every type knows where it is used, by operations at their request,
// result or data and by other types at their fields.
func TestRefusalsAndReferences(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"x": {
			"model.json":    `{"nightseam": 2, "types": {"Account": {"kind": "entity", "key": "id", "fields": [{"name": "id", "type": "string"}, {"name": "age", "type": "integer", "min": 18}]}, "Status": {"kind": "enum", "values": ["on", "off"]}, "Holder": {"kind": "record", "fields": [{"name": "account", "type": {"ref": "Account"}}, {"name": "status", "type": "Status"}]}}}`,
			"protocol.json": modeltest.Protocol(`"server": {"methods": {"get": {"request": "Account", "result": {"array": "Account"}, "errors": ["not_found", "denied"]}}, "events": {"changed": {"type": {"ref": "Account"}}}}, "client": {"methods": {"confirm": {"request": "Holder", "result": "Status"}}}, "errors": {"not_found": "No such account.", "denied": ""}`),
		},
	}))
	f := Build(render.Build(analysis.Resolve(world, "x")), nil)
	get := f.Server.Methods[0]
	if len(get.Frames.Refusals) != 2 || get.Frames.Refusals[0].Code != "not_found" || string(get.Frames.Refusals[0].Frame) != `{"version":1,"kind":"response","id":"c:1","error":{"code":"not_found","message":"No such account."}}` {
		t.Errorf("get's refusals are %+v", get.Frames.Refusals)
	}
	if string(get.Frames.Refusals[1].Frame) != `{"version":1,"kind":"response","id":"c:1","error":{"code":"denied","message":"denied"}}` {
		t.Errorf("an error with no description is not refused with its code: %s", get.Frames.Refusals[1].Frame)
	}
	if got := string(get.Frames.Response); got != `{"version":1,"kind":"response","id":"c:1","result":[{"id":"‹id›","age":18}]}` {
		t.Errorf("get's response does not hold the bound: %s", got)
	}
	confirm := f.Client.Methods[0]
	if !strings.Contains(string(confirm.Frames.Request), `"id":"s:1"`) || string(confirm.Frames.Response) != `{"version":1,"kind":"response","id":"s:1","result":"on"}` {
		t.Errorf("a method of the client is not the server's to call: %s %s", confirm.Frames.Request, confirm.Frames.Response)
	}
	if got := string(typed(f, "Holder").Example); got != `{"account":"‹id›","status":"on"}` {
		t.Errorf("a reference is not the key's value: %s", got)
	}
	if got := typed(f, "Account").UsedBy; !reflect.DeepEqual(got, []Reference{
		{Kind: "type", Name: "Holder", At: "account"},
		{Side: "server", Kind: "method", Name: "get", At: "request"},
		{Side: "server", Kind: "method", Name: "get", At: "result"},
		{Side: "server", Kind: "event", Name: "changed", At: "data"},
	}) {
		t.Errorf("Account is used by %+v", got)
	}
	if got := typed(f, "Status").UsedBy; !reflect.DeepEqual(got, []Reference{
		{Kind: "type", Name: "Holder", At: "status"},
		{Side: "client", Kind: "method", Name: "confirm", At: "result"},
	}) {
		t.Errorf("Status is used by %+v", got)
	}
}
