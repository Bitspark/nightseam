package check

import (
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

// probe is a session family; codex a second one; a test's family is
// checked in a world with both.
func world(family string, files map[string]string) *analysis.Family {
	families := map[string]map[string]string{
		"probe": {
			"model.json":    `{"nightseam": 2, "types": {"Payload": {"kind": "record", "fields": [{"name": "text", "type": "string"}]}, "Payloads": {"kind": "alias", "type": {"array": "Payload"}}}}`,
			"protocol.json": modeltest.Protocol(`"server": {"methods": {"echo": {"request": "Payload", "result": "Payload"}}, "events": {"changed": {"type": "Payload"}}}, "client": {"methods": {"reverse": {"request": "Payload", "result": "Payload"}}}, "errors": {"denied": "Denied."}`),
			"session.json":  `{"decides": ["echo"], "asks": ["reverse"], "conversation": {"event": "changed", "path": "text"}}`,
		},
		"codex": {
			"model.json":    `{"nightseam": 2, "types": {"Payload": {"kind": "record", "fields": [{"name": "n", "type": "integer"}]}}}`,
			"protocol.json": modeltest.Protocol(``),
			"session.json":  `{}`,
		},
		"carrier": {
			"model.json":    `{"nightseam": 2}`,
			"protocol.json": modeltest.Protocol(`"imports": ["probe"], "parameters": [{"name": "S", "of": "session"}], "types": {"Frame": {"kind": "record", "fields": [{"name": "message", "type": "S.Envelope"}]}, "Plain": {"kind": "record", "fields": []}}, "server": {"methods": {"relay": {"request": "Frame", "result": "probe.Envelope"}}}`),
		},
	}
	families[family] = files
	return analysis.Resolve(analysis.World(modeltest.World(families)), family)
}

func codes(diagnostics []diag.Diagnostic) string {
	var out []string
	for _, d := range diagnostics {
		out = append(out, d.Code+"@"+d.At().String())
	}
	return strings.Join(out, " ")
}

// TestCleanFamiliesPass: the world's own families have nothing to be told.
func TestCleanFamiliesPass(t *testing.T) {
	for _, name := range []string{"probe", "codex", "carrier"} {
		if got := Family(world(name, nil)); len(got) != 0 {
			t.Errorf("%s: %v", name, got)
		}
	}
}

// TestRules: each rule refuses what it holds a family to, at the file and
// pointer of the declaration at fault.
func TestRules(t *testing.T) {
	m := func(types string) string { return `{"nightseam": 2, "types": {` + types + `}}` }
	p := modeltest.Protocol
	for name, c := range map[string]struct {
		files map[string]string
		want  string // code@file#pointer, one per expected diagnostic, space-separated, in sorted order
	}{
		"unknown type":                        {map[string]string{"model.json": m(`"A": {"kind": "record", "fields": [{"name": "x", "type": "Nope"}]}`)}, "unresolved_type@model.json#/types/A/fields/0/type"},
		"unknown type in an alias":            {map[string]string{"model.json": m(`"A": {"kind": "alias", "type": {"map": "Nope"}}`)}, "unresolved_type@model.json#/types/A/type"},
		"self import":                         {map[string]string{"model.json": `{"nightseam": 2, "imports": ["x"]}`}, "self_import@model.json#/imports/0"},
		"unknown import":                      {map[string]string{"model.json": `{"nightseam": 2, "imports": ["nobody"]}`}, "unresolved_import@model.json#/imports/0"},
		"import not declared":                 {map[string]string{"model.json": m(`"A": {"kind": "record", "fields": [{"name": "p", "type": "probe.Payload"}]}`)}, "unresolved_type@model.json#/types/A/fields/0/type"},
		"unknown imported type":               {map[string]string{"model.json": `{"nightseam": 2, "imports": ["probe"], "types": {"A": {"kind": "record", "fields": [{"name": "p", "type": "probe.Nope"}]}}}`}, "unresolved_type@model.json#/types/A/fields/0/type"},
		"tier violation across families":      {map[string]string{"model.json": `{"nightseam": 2, "imports": ["probe"], "types": {"A": {"kind": "record", "fields": [{"name": "p", "type": "probe.Envelope"}]}}}`}, "tier_violation@model.json#/types/A/fields/0/type"},
		"tier violation within a family":      {map[string]string{"model.json": m(`"A": {"kind": "record", "fields": [{"name": "p", "type": "B"}]}`), "protocol.json": p(`"types": {"B": {"kind": "record", "fields": []}}`)}, "tier_violation@model.json#/types/A/fields/0/type"},
		"inheritance across tiers":            {map[string]string{"model.json": m(`"A": {"kind": "record", "extends": ["B"], "fields": []}`), "protocol.json": p(`"types": {"B": {"kind": "record", "fields": []}}`)}, "tier_violation@model.json#/types/A/extends/0"},
		"a parameter in the model tier":       {map[string]string{"model.json": m(`"A": {"kind": "record", "fields": [{"name": "m", "type": "S.Envelope"}]}`), "protocol.json": p(`"parameters": [{"name": "S", "of": "session"}], "types": {"B": {"kind": "record", "fields": [{"name": "m", "type": "S.Envelope"}]}}`)}, "tier_violation@model.json#/types/A/fields/0/type"},
		"unknown parent":                      {map[string]string{"model.json": m(`"A": {"kind": "record", "extends": ["Nope"], "fields": []}`)}, "unresolved_type@model.json#/types/A/extends/0"},
		"parent not a record":                 {map[string]string{"model.json": m(`"A": {"kind": "record", "extends": ["E"], "fields": []}, "E": {"kind": "enum", "values": ["v"]}`)}, "invalid_inheritance@model.json#/types/A/extends/0"},
		"a value cycle":                       {map[string]string{"model.json": m(`"A": {"kind": "record", "fields": [{"name": "b", "type": {"array": "B"}}]}, "B": {"kind": "record", "fields": [{"name": "a", "type": "A"}]}`)}, "cyclic_type@model.json#/types/B"},
		"a field twice":                       {map[string]string{"model.json": m(`"A": {"kind": "record", "fields": [{"name": "x", "type": "string"}]}, "B": {"kind": "record", "extends": ["A"], "fields": [{"name": "x", "type": "string"}]}`)}, "field_collision@model.json#/types/B/fields/0/name"},
		"an inherited key":                    {map[string]string{"model.json": m(`"Base": {"kind": "record", "fields": [{"name": "id", "type": "string"}]}, "A": {"kind": "entity", "key": "id", "extends": ["Base"], "fields": []}`)}, ""},
		"a pattern that is no expression":     {map[string]string{"model.json": m(`"A": {"kind": "record", "fields": [{"name": "a", "type": "string", "pattern": "("}]}`)}, "invalid_constraint@model.json#/types/A/fields/0/pattern"},
		"a bad key":                           {map[string]string{"model.json": m(`"A": {"kind": "entity", "key": "id", "fields": [{"name": "id", "type": "string", "nullable": true}]}, "B": {"kind": "entity", "key": "nope", "fields": []}`)}, "invalid_key@model.json#/types/A/key invalid_key@model.json#/types/B/key"},
		"a ref to a record":                   {map[string]string{"model.json": m(`"A": {"kind": "record", "fields": [{"name": "r", "type": {"ref": "A"}}, {"name": "n", "type": {"ref": "Nope"}}]}`)}, "invalid_ref@model.json#/types/A/fields/0/type unresolved_type@model.json#/types/A/fields/1/type"},
		"constraints that do not fit":         {map[string]string{"model.json": m(`"A": {"kind": "record", "fields": [{"name": "a", "type": "string", "min": 1}, {"name": "b", "type": "integer", "length": {"max": 3}}, {"name": "c", "type": "boolean", "pattern": "x"}, {"name": "d", "type": "string", "unique": true}]}`)}, "invalid_constraint@model.json#/types/A/fields/0 invalid_constraint@model.json#/types/A/fields/1/length invalid_constraint@model.json#/types/A/fields/2/pattern invalid_constraint@model.json#/types/A/fields/3/unique"},
		"Envelope declared":                   {map[string]string{"model.json": m(`"Envelope": {"kind": "record", "fields": []}`), "protocol.json": p(``)}, "reserved_name@model.json#/types/Envelope"},
		"parameters":                          {map[string]string{"model.json": m(`"S": {"kind": "enum", "values": ["v"]}`), "protocol.json": p(`"parameters": [{"name": "S", "of": "session"}, {"name": "S", "of": "thing"}, {"name": "T", "of": "session"}], "types": {"A": {"kind": "record", "fields": [{"name": "m", "type": "S.Envelope"}]}}`)}, "reserved_name@protocol.json#/parameters/0/name duplicate_parameter@protocol.json#/parameters/1/name reserved_name@protocol.json#/parameters/1/name unknown_role@protocol.json#/parameters/1/of unused_parameter@protocol.json#/parameters/2/name"},
		"unknown parameter":                   {map[string]string{"model.json": m(``), "protocol.json": p(`"types": {"A": {"kind": "record", "fields": [{"name": "m", "type": "S.Envelope"}]}}`)}, "unresolved_parameter@protocol.json#/types/A/fields/0/type"},
		"a drawn type not every member has":   {map[string]string{"model.json": m(``), "protocol.json": p(`"parameters": [{"name": "S", "of": "session"}], "types": {"A": {"kind": "record", "fields": [{"name": "p", "type": "S.Payloads"}, {"name": "q", "type": "S.Nope"}]}}`)}, "unresolved_type@protocol.json#/types/A/fields/0/type unresolved_type@protocol.json#/types/A/fields/0/type unresolved_type@protocol.json#/types/A/fields/1/type unresolved_type@protocol.json#/types/A/fields/1/type"},
		"a plain reference to a generic type": {map[string]string{"model.json": m(``), "protocol.json": p(`"imports": ["carrier"], "parameters": [{"name": "A", "of": "session"}, {"name": "B", "of": "session"}], "types": {"X": {"kind": "record", "fields": [{"name": "f", "type": "carrier.Frame"}, {"name": "a", "type": "A.Envelope"}, {"name": "b", "type": "B.Envelope"}]}}`)}, "ambiguous_application@protocol.json#/types/X/fields/0/type"},
		"applications":                        {map[string]string{"model.json": m(``), "protocol.json": p(`"imports": ["carrier"], "parameters": [{"name": "A", "of": "session"}], "types": {"X": {"kind": "record", "fields": [{"name": "f", "type": {"apply": "carrier.Plain", "with": {}}}, {"name": "g", "type": {"apply": "carrier.Frame", "with": {}}}, {"name": "h", "type": {"apply": "carrier.Frame", "with": {"S": "Q", "T": "A"}}}, {"name": "i", "type": {"apply": "carrier.Frame", "with": {"S": "x"}}}, {"name": "j", "type": {"apply": "carrier.Frame", "with": {"S": "nobody"}}}, {"name": "k", "type": {"apply": "probe.Payload", "with": {}}}, {"name": "l", "type": {"apply": "carrier.Nope", "with": {}}}]}}`)}, "needless_application@protocol.json#/types/X/fields/0/type/apply unbound_parameter@protocol.json#/types/X/fields/1/type/with unresolved_parameter@protocol.json#/types/X/fields/2/type/with/S unresolved_parameter@protocol.json#/types/X/fields/2/type/with/T self_slot@protocol.json#/types/X/fields/3/type/with/S unresolved_type@protocol.json#/types/X/fields/4/type/with/S unresolved_type@protocol.json#/types/X/fields/5/type/apply unresolved_type@protocol.json#/types/X/fields/6/type/apply"},
		"nothing to bind":                     {map[string]string{"model.json": m(``), "protocol.json": p(`"parameters": [{"name": "S", "of": "session"}], "types": {"A": {"kind": "record", "fields": [{"name": "m", "type": "S.Envelope"}]}}`), "session.json": `{}`}, ""},
		"operations":                          {map[string]string{"model.json": m(`"E": {"kind": "enum", "values": ["v"]}`), "protocol.json": p(`"server": {"methods": {"a": {"request": "E", "result": "string"}, "b": {"request": "Nope", "result": "Nope", "errors": ["nope"]}}, "events": {"a": {"type": "string"}}, "crud": {"E": ["list"]}}, "client": {"events": {"b": {"type": "string"}}}`)}, "operation_collision@protocol.json#/client/events/b unsupported@protocol.json#/server/crud invalid_request@protocol.json#/server/methods/a/request unknown_error@protocol.json#/server/methods/b/errors/0 unresolved_type@protocol.json#/server/methods/b/request unresolved_type@protocol.json#/server/methods/b/result"},
		"session names":                       {map[string]string{"model.json": m(``), "protocol.json": p(`"server": {"methods": {"a": {"result": "string"}}}, "client": {"methods": {"b": {"result": "string"}}}`), "session.json": `{"decides": ["nope"], "asks": ["a", "nope"], "conversation": {"event": "nope", "path": "x"}}`}, "invalid_side@session.json#/asks/0 unknown_operation@session.json#/asks/1 unknown_operation@session.json#/conversation/event unknown_operation@session.json#/decides/0"},
		"overrides":                           {map[string]string{"model.json": m(`"A": {"kind": "record", "fields": [{"name": "x", "type": "string"}]}, "E": {"kind": "enum", "values": ["v"]}`), "protocol.json": p(`"server": {"methods": {"m": {"result": "string"}, "both": {"result": "string"}}, "events": {"e": {"type": "string"}, "both": {"type": "string"}}}, "errors": {"c": "C."}`), "go.json": `{"names": {"A": "a", "A.x": "X", "E.v": "V", "m": "M", "e": "E", "errors.c": "C", "A.y": "Y", "Nope": "N", "both": "B", "errors.d": "D", "Envelope": "Env"}}`, "typescript.json": `{"types": {}}`}, "unknown_override@go.json#/names/A.y unknown_override@go.json#/names/Envelope unknown_override@go.json#/names/Nope unknown_override@go.json#/names/both unknown_override@go.json#/names/errors.d invalid_override@typescript.json#"},
	} {
		t.Run(name, func(t *testing.T) {
			got := codes(Family(world("x", c.files)))
			if got != c.want {
				t.Fatalf("got:\n%s\nwant:\n%s", strings.ReplaceAll(got, " ", "\n"), strings.ReplaceAll(c.want, " ", "\n"))
			}
		})
	}
}

// TestImportCycles: two families importing each other are refused on both
// sides, and a chain that returns is too.
func TestImportCycles(t *testing.T) {
	w := analysis.World(modeltest.World(map[string]map[string]string{
		"a": {"model.json": `{"nightseam": 2, "imports": ["b"]}`},
		"b": {"model.json": `{"nightseam": 2, "imports": ["c"]}`},
		"c": {"model.json": `{"nightseam": 2, "imports": ["a"]}`},
		"d": {"model.json": `{"nightseam": 2, "imports": ["a"]}`},
	}))
	for name, want := range map[string]string{"a": "import_cycle@model.json#/imports/0", "c": "import_cycle@model.json#/imports/0", "d": ""} {
		if got := codes(Family(analysis.Resolve(w, name))); got != want {
			t.Errorf("%s: %s", name, got)
		}
	}
}

// TestLocate: an override's path key finds what it names.
func TestLocate(t *testing.T) {
	f := world("x", map[string]string{
		"model.json":    `{"nightseam": 2, "types": {"A": {"kind": "record", "fields": [{"name": "x", "type": "string"}]}, "E": {"kind": "enum", "values": ["v"]}}}`,
		"protocol.json": modeltest.Protocol(`"server": {"methods": {"m": {"result": "string"}}}, "client": {"events": {"m": {"type": "string"}}}, "errors": {"c": "C."}`),
	})
	for key, want := range map[string]string{"A": "model.json#/types/A", "A.x": "model.json#/types/A/fields/0", "E.v": "model.json#/types/E/values/0", "errors.c": "protocol.json#/errors/c"} {
		if at, ok := f.Locate(key); !ok || at.String() != want {
			t.Errorf("%s located at %s, %v", key, at, ok)
		}
	}
	for _, key := range []string{"m", "Nope", "A.nope", "errors.nope", "Envelope", "Envelope.kind"} {
		if _, ok := f.Locate(key); ok {
			t.Errorf("%s was located", key)
		}
	}
	var _ model.TypeExpr
}
