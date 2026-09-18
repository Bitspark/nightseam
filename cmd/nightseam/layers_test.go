package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bitspark/nighthall/tools/go/generate-api/internal/contract"
)

// writeLayers splits the probe contract into its layer files under a
// checkout, as a family is declared: types in dto, operations in rpc, and,
// when asked, a sess layer that makes it a session family.
func writeLayers(t *testing.T, root, family string, sess bool) {
	t.Helper()
	var probe map[string]any
	if err := json.Unmarshal([]byte(probeContract), &probe); err != nil {
		t.Fatal(err)
	}
	head := map[string]any{"schema_version": probe["schema_version"], "profile": probe["profile"], "name": family}
	write := func(layer string, body map[string]any) {
		for key, value := range head {
			body[key] = value
		}
		body["layer"] = layer
		data, _ := json.Marshal(body)
		writeFixture(t, root, "api/contracts/"+family+"."+layer+".json", data)
	}
	write(contract.LayerDTO, map[string]any{"types": probe["types"]})
	write(contract.LayerRPC, map[string]any{"methods": probe["methods"], "events": probe["events"], "errors": probe["errors"]})
	if sess {
		write(contract.LayerSess, map[string]any{"session": map[string]any{"decides": []any{"echo"}, "asks": []any{"reverse"}, "conversation": map[string]any{"event": "changed", "path": "text"}}})
	}
}

// TestLayerFilesAreLoadedMergedAndRendered: a family declared in layer files
// validates and renders as one; its sess layer becomes the Caller interface's
// companions, Decides, Asks and Conversation, in both languages.
func TestLayerFilesAreLoadedMergedAndRendered(t *testing.T) {
	root := t.TempDir()
	writeLayers(t, root, "probe", true)
	out, _, err := run(t, root, "validate")
	if err != nil || !strings.Contains(out, "1 contracts; valid") {
		t.Fatalf("validate: %v\n%s", err, out)
	}
	if out, _, err := run(t, root, "generate"); err != nil || strings.Count(out, "generated ") != 8 {
		t.Fatalf("generate: %v\n%s", err, out)
	}
	client, err := os.ReadFile(filepath.Join(root, "api", "go", "probe-client", "client_generated.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"type Caller interface", "var _ Caller = (*Client)(nil)", `case "echo":`, "func Decides(method string) bool", "func Asks(method string) bool", `var Conversation = struct{ Event, Path string }{"changed", "text"}`} {
		if !strings.Contains(string(client), want) {
			t.Errorf("the Go client lacks %s", want)
		}
	}
	index, err := os.ReadFile(filepath.Join(root, "api", "ts", "probe-client", "src", "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"export interface Caller {", "export class Client implements Caller {", `export const decides: ReadonlySet<string> = new Set(["echo"]);`, `export const asks: ReadonlySet<string> = new Set(["reverse"]);`, `export const conversation = { event: "changed", path: "text" } as const;`} {
		if !strings.Contains(string(index), want) {
			t.Errorf("the TypeScript client lacks %s", want)
		}
	}
	if out, errs, err := run(t, root, "check"); err != nil || out != "" || errs != "" {
		t.Fatalf("check after generate: %v\n%s%s", err, out, errs)
	}
}

// TestLoaderHoldsTheLayers: a single-file contract is not a family; a file
// under one family naming another is refused; a dto that refers upward is
// a layer violation the validate command reports at the reference.
func TestLoaderHoldsTheLayers(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "api/contracts/probe.json", []byte(probeContract))
	if _, _, err := run(t, root, "validate"); err == nil || !strings.Contains(err.Error(), "no contracts in") {
		t.Fatalf("a single-file contract was taken for a family: %v", err)
	}
	writeLayers(t, root, "probe", false)
	writeFixture(t, root, "api/contracts/other.dto.json", []byte(strings.Replace(`{"schema_version":1,"profile":"nighthall.duplex/1","name":"probe","layer":"dto","types":{}}`, "x", "x", 1)))
	if _, _, err := run(t, root, "validate", "other"); err == nil || !strings.Contains(err.Error(), "names API") {
		t.Fatalf("a layer file naming another family passed: %v", err)
	}
	writeFixture(t, root, "api/contracts/other.dto.json", []byte(`{"schema_version":1,"profile":"nighthall.duplex/1","name":"other","layer":"rpc","types":{}}`))
	if _, _, err := run(t, root, "validate", "other"); err == nil || !strings.Contains(err.Error(), "wrong_layer") && !strings.Contains(err.Error(), "declares layer") {
		t.Fatalf("a file of the wrong layer passed: %v", err)
	}
	writeFixture(t, root, "api/contracts/other.dto.json", []byte(`{"schema_version":1,"profile":"nighthall.duplex/1","name":"other","layer":"dto","types":{"Holder":{"kind":"record","fields":[{"name":"message","type":{"envelope":"probe"}}]}}}`))
	_, errs, err := run(t, root, "validate", "other")
	if err == nil || !strings.Contains(errs, "other /types/Holder/fields/0/type:") || !strings.Contains(errs, "[layer_violation]") {
		t.Fatalf("a dto holding an envelope slot passed: %v\n%s", err, errs)
	}
}
