package typescript

import (
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

func TestWireModelsPreserveBothDirectionsAndEventFacets(t *testing.T) {
	f := family(map[string]string{
		"model.json":    fixtureModel,
		"protocol.json": modeltest.Protocol(`"server":{"methods":{"run":{"request":"Input","result":"Result"}},"events":{"changed":{"type":"Result"}}},"client":{"methods":{"reverse":{"request":"Input","result":"Result"}},"events":{"noticed":{"type":"Input"}}}`),
	})
	files, err := New(Config{Scope: "@example"}).Render(f)
	if err != nil {
		t.Fatal(err)
	}
	var types, client, binding string
	for _, file := range files {
		switch file.Path {
		case "api/ts/x-client/src/types.ts":
			types = string(file.Data)
		case "api/ts/x-client/src/index.ts":
			client = string(file.Data)
		case "api/ts/x-binding/src/index.ts":
			binding = string(file.Data)
		}
	}
	for _, want := range []string{"interface ServerMethods", "interface ClientMethods", "interface ServerEvents", "interface ClientEvents", "type ServerModel = (remote: Client) => Server", "type ClientModel = (remote: Server) => Client"} {
		if !strings.Contains(types, want) {
			t.Errorf("shared model surface lacks %q", want)
		}
	}
	for role, module := range map[string]string{"client": client, "server": binding} {
		for _, want := range []string{"export function toWire", "export async function fromWire", "wirePair(", "registerWire(", "model already bound"} {
			if !strings.Contains(module, want) {
				t.Errorf("%s adapter lacks %q", role, want)
			}
		}
		for _, want := range []string{"const propagator = context.options?.propagator;", "const requestTimeoutMs = context.options?.requestTimeoutMs;", "timeoutMs: context?.timeoutMs ?? requestTimeoutMs", `meta: context?.outgoingMeta, observer, propagator, family: "x"`} {
			if !strings.Contains(module, want) {
				t.Errorf("%s adapter drops configured model operation options %q", role, want)
			}
		}
		for _, removed := range []string{"DuplexPeer", "FrameConnection", "scopeOf(", "function serve", "function install", "static async dial", "static async attach", "static async open"} {
			if strings.Contains(module, removed) {
				t.Errorf("%s adapter retains transport entry point %q", role, removed)
			}
		}
	}
}
