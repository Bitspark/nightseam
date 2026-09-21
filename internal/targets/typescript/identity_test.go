package typescript

import (
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

func TestWireIdentityPreparationPrecedesModelBinding(t *testing.T) {
	f := family(map[string]string{
		"model.json":    fixtureModel,
		"protocol.json": modeltest.Protocol(`"server":{"methods":{"run":{"request":"Input","result":"Result"}},"events":{"changed":{"type":"Result"}}},"client":{"methods":{"reverse":{"request":"Input","result":"Result"}},"events":{"noticed":{"type":"Input"}}}`),
	})
	files, err := New(Config{Scope: "@example"}).Render(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Path, "/src/index.ts") {
			continue
		}
		side, opposite := "Client", "Server"
		if strings.Contains(file.Path, "-binding/") {
			side, opposite = "Server", "Client"
		}
		source := string(file.Data)
		for _, want := range []string{
			`const identity = { path: "x", digest: declarationDigest(validateWire, slots) };`,
			"export function prepareFromWire(wire: Wire, context: AdapterContext)",
			"complete(options?: WireCallOptions): Promise<Protocol." + side + "Model>",
			"const gate = prepareIdentity(wire, adapter.identity, adapter.options);",
			"registerWire(binding, [IDENTITY_METHOD], { request: identityHandler(adapter.identity) })",
			"adapter.bind" + opposite + "(gate.wire, () => implementation!)",
			"await gate.check(options);",
			"adapter.validate" + opposite + "(remote);",
			"implementation = remote;",
			"gate.ready();",
			"return adapter.proxy" + side + "(gate.wire);",
			"const target = implementation();",
			"return () => { for (const remove of detach.reverse()) remove(); };",
		} {
			if !strings.Contains(source, want) {
				t.Errorf("%s lacks %q", file.Path, want)
			}
		}
		_, local, _ := strings.Cut(source, "export function toWire")
		local, _, _ = strings.Cut(local, "export function prepareFromWire")
		if responder, factory := strings.Index(local, "identityHandler(adapter.identity)"), strings.Index(local, "const implementation = model("); responder < 0 || factory < 0 || responder > factory {
			t.Errorf("%s invokes the local model before installing its identity responder", file.Path)
		}
		_, prepare, _ := strings.Cut(source, "export function prepareFromWire")
		if registration, completion := strings.Index(prepare, "adapter.bind"+opposite+"(gate.wire"), strings.Index(prepare, "async complete(options?: WireCallOptions)"); registration < 0 || completion < 0 || registration > completion {
			t.Errorf("%s does not install opposite receivers synchronously", file.Path)
		}
	}
}

func TestWireIdentityConstructionNamesAreReserved(t *testing.T) {
	for _, name := range []string{"prepareFromWire", "prepareIdentity", "identityHandler", "declarationDigest", "IDENTITY_METHOD", "WireCallOptions"} {
		t.Run(name, func(t *testing.T) {
			diagnostics := check(map[string]string{
				"model.json":      fixtureModel,
				"typescript.json": `{"names":{"Input":"` + name + `"}}`,
			})
			if !has(diagnostics, "reserved_name", "typescript.json#/names/Input") {
				t.Fatalf("generated identity name is not reserved: %v", diagnostics)
			}
		})
	}
}
