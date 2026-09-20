package typescript

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

func TestRenderServerBinding(t *testing.T) {
	f := family(map[string]string{"model.json": fixtureModel, "protocol.json": fixtureProtocol(`"client":{"methods":{"reverse":{"request":"Input","result":"Result"}},"events":{"noticed":{"type":"Input"}}}`)})
	files, err := New(Config{Scope: "@example"}).Render(f)
	if err != nil {
		t.Fatal(err)
	}
	var binding, manifest string
	for _, file := range files {
		switch file.Path {
		case "api/ts/x-binding/src/index.ts":
			binding = string(file.Data)
		case "api/ts/x-binding/package.json":
			manifest = string(file.Data)
		}
	}
	for _, want := range []string{"export interface Handler", "export interface Events", "export class Remote", "export async function serve", "run(params: Protocol.Input, remote: Remote, context: RequestContext)", "async reverse(", "async emitChanged(", "onNoticed(", "role: 'server'"} {
		if !strings.Contains(binding, want) {
			t.Errorf("server binding lacks %q:\n%s", want, binding)
		}
	}
	for _, want := range []string{`"name":"@example/x-binding"`, `"@example/x-client":"file:../x-client"`} {
		if !strings.Contains(manifest, want) {
			t.Errorf("binding manifest lacks %q: %s", want, manifest)
		}
	}
}

// Placement changes package directories without changing import names, and
// both directories remain owned so regeneration can remove stale output.
func TestBindingPlacement(t *testing.T) {
	config := Config{Scope: "@example", Layout: Layout{Client: "packages/{family}/client", Binding: "servers/{family}"}, Place: map[string]Layout{"x": {Binding: "special/{family}/binding"}}}
	target := New(config)
	f := family(map[string]string{"model.json": fixtureModel, "protocol.json": fixtureProtocol(``)})
	files, err := target.Render(f)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct{ Dependencies map[string]string }
	for _, file := range files {
		if file.Path == "special/x/binding/package.json" {
			if err := json.Unmarshal(file.Data, &manifest); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := manifest.Dependencies["@example/x-client"]; got != "file:../../../packages/x/client" {
		t.Fatalf("binding protocol dependency = %q", got)
	}
	if got := target.Owns("x"); !slices.Equal(got, []string{"packages/x/client", "special/x/binding"}) {
		t.Fatalf("owned paths = %v", got)
	}
	for _, path := range []string{"packages/x/client/src/types.ts", "special/x/binding/src/index.ts", "servers/y/src/index.ts"} {
		if _, ok := target.Family(path); !ok {
			t.Errorf("generated path %s is not owned", path)
		}
	}
	for _, path := range []string{"servers/x/src/index.ts", "special/x/binding/node_modules/dep/index.ts"} {
		if _, ok := target.Family(path); ok {
			t.Errorf("unowned path %s was claimed", path)
		}
	}
	if config.Place["x"].Client != "" {
		t.Fatal("settling config mutated the caller's placement")
	}
	for _, layout := range []Layout{{Client: "api/{family}", Binding: "api/{family}"}, {Binding: "../{family}"}, {Binding: "missing-family"}} {
		if err := (Config{Scope: "@example", Layout: layout}).Validate(); err == nil {
			t.Errorf("invalid layout accepted: %+v", layout)
		}
	}
}

func TestDataFamilyHasNoBinding(t *testing.T) {
	files, err := New(Config{Scope: "@example"}).Render(family(map[string]string{"model.json": fixtureModel}))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.Contains(file.Path, "-binding/") {
			t.Fatalf("data-only family emitted server binding %s", file.Path)
		}
	}
}

func TestBindingNamesAreChecked(t *testing.T) {
	for name, protocol := range map[string]string{
		"then":  `"client":{"methods":{"then":{"result":"string"}}}`,
		"close": `"client":{"methods":{"close":{"result":"string"}}}`,
		"event": `"client":{"methods":{"emit_changed":{"result":"string"}}},"server":{"events":{"changed":{"type":"string"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if diagnostics := check(map[string]string{"model.json": fixtureModel, "protocol.json": modeltest.Protocol(protocol)}); len(diagnostics) == 0 {
				t.Fatal("a collision in Remote was accepted")
			}
		})
	}
}
