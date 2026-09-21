package typescript

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

func TestEventFieldsRejectObjectMembers(t *testing.T) {
	for _, name := range []string{
		"constructor", "__defineGetter__", "__defineSetter__", "hasOwnProperty",
		"__lookupGetter__", "__lookupSetter__", "isPrototypeOf", "propertyIsEnumerable",
		"toString", "valueOf", "__proto__", "toLocaleString",
	} {
		t.Run(name, func(t *testing.T) {
			diagnostics := check(map[string]string{
				"model.json":      fixtureModel,
				"protocol.json":   fixtureProtocol(""),
				"typescript.json": fmt.Sprintf(`{"names":{"changed":%q}}`, name),
			})
			if !has(diagnostics, "reserved_name", "typescript.json#/names/changed") {
				t.Fatalf("an event facet member inherited from Object was accepted: %v", diagnostics)
			}
		})
	}
}

func TestEventFacetsKeepRequiredNamesAndReceiveDirections(t *testing.T) {
	files, err := New(Config{Scope: "@example"}).Render(family(map[string]string{
		"model.json":      fixtureModel,
		"protocol.json":   modeltest.Protocol(`"server":{"events":{"to_string":{"type":"Result"}}},"client":{"events":{"has_own_property":{"type":"Input"}}}`),
		"typescript.json": `{"names":{"to_string":"textChanged","has_own_property":"propertyChanged"}}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	types := string(files[0].Data)
	for _, want := range []string{
		"export interface ServerEvents {\n  propertyChanged(data: Input, context?: WireModelContext): void | Promise<void>;\n}",
		"export interface ClientEvents {\n  textChanged(data: Result, context?: WireModelContext): void | Promise<void>;\n}",
	} {
		if !strings.Contains(types, want) {
			t.Errorf("required event facet or receive direction missing %q:\n%s", want, types)
		}
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Path, "/src/index.ts") {
			continue
		}
		for _, want := range []string{
			`registerWire(wire, ["to_string"], {`, `registerWire(wire, ["has_own_property"], {`,
			"await implementation.events.textChanged(raw as Protocol.Result, context)", "await implementation.events.propertyChanged(raw as Protocol.Input, context)",
			`if (!hasModelHandler(implementation.events, "textChanged"))`, `if (!hasModelHandler(implementation.events, "propertyChanged"))`,
		} {
			if !strings.Contains(string(file.Data), want) {
				t.Errorf("%s lacks %q", file.Path, want)
			}
		}
	}
}

func TestEventFieldCollisionCanBeOverridden(t *testing.T) {
	files := map[string]string{
		"model.json": fixtureModel,
		"protocol.json": modeltest.Protocol(`"server":{"events":{"to_string":{"type":"Result"}}},
			"client":{"events":{"has_own_property":{"type":"Result"}}}`),
	}
	if diagnostics := check(files); !has(diagnostics, "reserved_name", "protocol.json#/server/events/to_string") {
		t.Fatalf("a conventional Events field inherited from Object was accepted: %v", diagnostics)
	}
	if diagnostics := check(files); !has(diagnostics, "reserved_name", "protocol.json#/client/events/has_own_property") {
		t.Fatalf("a binding Events field inherited from Object was accepted: %v", diagnostics)
	}
	files["typescript.json"] = `{"names":{"to_string":"textChanged","has_own_property":"propertyChanged"}}`
	if diagnostics := check(files); len(diagnostics) != 0 {
		t.Fatalf("safe event overrides were refused: %v", diagnostics)
	}
}
