package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeLayers copies the probe family's layer files of the previous
// language from testdata/corpus-v1 into a checkout under another name —
// dto and rpc, and, when asked, sess — for the tests of upgrade and of what
// the tool says to a checkout that still has them.
func writeLayers(t *testing.T, root, family string, sess bool) {
	t.Helper()
	layers := []string{"dto", "rpc"}
	if sess {
		layers = append(layers, "sess")
	}
	for _, layer := range layers {
		data, err := os.ReadFile(filepath.Join(legacyCorpusRoot, "api/contracts", "probe."+layer+".json"))
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, root, "api/contracts/"+family+"."+layer+".json", []byte(strings.Replace(string(data), `"name": "probe"`, `"name": "`+family+`"`, 1)))
	}
}

// writeFamily copies a family of testdata/families into a checkout.
func writeFamily(t *testing.T, root, family string) {
	t.Helper()
	dir := filepath.Join(familiesRoot, "api/contracts", family)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, root, "api/contracts/"+family+"/"+entry.Name(), data)
	}
}

// TestTierFilesAreLoadedAndRendered: a family declared in tier files
// validates and renders as one; its session tier becomes the Caller
// interface's companions, Decides, Asks and Conversation, in both
// languages.
func TestTierFilesAreLoadedAndRendered(t *testing.T) {
	root := t.TempDir()
	writeFamily(t, root, "probe")
	writeFixture(t, root, "api/contracts/probe/session.json", []byte(`{"decides": ["echo"], "asks": ["reverse"], "conversation": {"event": "changed", "path": "text"}}`))
	if out, _, err := run(t, root, "validate"); err != nil || out != "1 families; valid\n" {
		t.Fatalf("validate: %v\n%s", err, out)
	}
	if out, _, err := run(t, root, "generate"); err != nil || strings.Count(out, "generated ") != 8 {
		t.Fatalf("generate: %v\n%s", err, out)
	}
	client, err := os.ReadFile(filepath.Join(root, "api/go/probe-client/client_generated.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"type Caller interface", "var _ Caller = (*Client)(nil)", `case "echo":`, "func Decides(method string) bool", "func Asks(method string) bool", `var Conversation = struct{ Event, Path string }{"changed", "text"}`} {
		if !strings.Contains(string(client), want) {
			t.Errorf("the Go client lacks %s", want)
		}
	}
	index, err := os.ReadFile(filepath.Join(root, "api/ts/probe-client/src/index.ts"))
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

// TestLoaderHoldsTheTiers: a checkout is held to the directory form and
// the tier rule, each refusal naming the file and what to do.
func TestLoaderHoldsTheTiers(t *testing.T) {
	root := t.TempDir()
	writeLayers(t, root, "probe", false)
	_, errs, err := run(t, root, "validate")
	if err == nil || !strings.Contains(errs, "probe/probe.dto.json#: probe is declared in the dto layer file of the previous declaration language; run nightseam upgrade. [layer_file]") {
		t.Fatalf("a layer file was not named: %v\n%s", err, errs)
	}
	os.Remove(filepath.Join(root, "api/contracts/probe.dto.json"))
	os.Remove(filepath.Join(root, "api/contracts/probe.rpc.json"))
	writeFamily(t, root, "probe")
	writeFixture(t, root, "api/contracts/probe/rust.json", []byte(`{}`))
	if _, errs, err := run(t, root, "validate"); err == nil || !strings.Contains(errs, "probe/rust.json#:") || !strings.Contains(errs, "[unknown_file]") {
		t.Fatalf("an unknown file passed: %v\n%s", err, errs)
	}
	os.Remove(filepath.Join(root, "api/contracts/probe/rust.json"))
	writeFixture(t, root, "api/contracts/probe/model.json", []byte(`{"nightseam": 2, "types": {"Holder": {"kind": "record", "fields": [{"name": "message", "type": "S.Envelope"}]}}}`))
	if _, errs, err := run(t, root, "validate"); err == nil || !strings.Contains(errs, "probe/model.json#/types/Holder/fields/0/type:") || !strings.Contains(errs, "[tier_violation]") {
		t.Fatalf("a parameter's type in the model tier passed: %v\n%s", err, errs)
	}
}

// TestParametersAreLoadedFromTheProtocolTier: a family generic in a
// session family renders its types with the parameter's uses as Go type
// parameters.
func TestParametersAreLoadedFromTheProtocolTier(t *testing.T) {
	root := t.TempDir()
	writeFamily(t, root, "probe")
	writeFamily(t, root, "carrier")
	if out, _, err := run(t, root, "validate"); err != nil || out != "2 families; valid\n" {
		t.Fatalf("validate: %v\n%s", err, out)
	}
	if _, _, err := run(t, root, "generate", "carrier"); err != nil {
		t.Fatal(err)
	}
	types, err := os.ReadFile(filepath.Join(root, "api/go/carrier-protocol/types_generated.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(types), "type Frame[SEnvelope any] struct") {
		t.Fatalf("the carrier's Frame is not generic:\n%s", types)
	}
}

// TestGenerateRemovesWhatNothingRenders: a rendering a family no longer
// produces — the family removed, or a file a target no longer writes — is
// what check reports and generate removes, the directory it leaves empty
// with it; what a package manager installed beside a client is nobody's
// and stays; a family that exists and was not chosen this time is not
// touched.
func TestGenerateRemovesWhatNothingRenders(t *testing.T) {
	root := t.TempDir()
	writeFamily(t, root, "probe")
	writeFamily(t, root, "codex")
	if _, _, err := run(t, root, "generate"); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "api/ts/probe-client/node_modules/@nightseam/runtime/package.json", []byte(`{}`))
	writeFixture(t, root, "api/go/probe-protocol/stray_generated.go", []byte("package probeprotocol\n"))
	if err := os.RemoveAll(filepath.Join(root, "api/contracts/codex")); err != nil {
		t.Fatal(err)
	}
	_, errs, err := run(t, root, "check")
	if err == nil || !strings.Contains(errs, "generated output nothing renders: api/go/codex-protocol/types_generated.go") || !strings.Contains(errs, "generated output nothing renders: api/go/probe-protocol/stray_generated.go") || strings.Contains(errs, "node_modules") {
		t.Fatalf("check did not report what nothing renders: %v\n%s", err, errs)
	}
	out, _, err := run(t, root, "generate", "probe")
	if err != nil || !strings.Contains(out, "removed api/go/probe-protocol/stray_generated.go") || !strings.Contains(out, "removed api/ts/codex-client/src/index.ts") {
		t.Fatalf("generate did not remove: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(root, "api/go/codex-protocol")); !os.IsNotExist(err) {
		t.Fatal("the removed family's empty directory remains")
	}
	if _, err := os.Stat(filepath.Join(root, "api/ts/probe-client/node_modules/@nightseam/runtime/package.json")); err != nil {
		t.Fatal("node_modules was touched")
	}
	if out, errs, err := run(t, root, "check"); err != nil || out != "" || errs != "" {
		t.Fatalf("check after generate: %v\n%s%s", err, out, errs)
	}
	// A family that exists and was not chosen is left as it is.
	writeFamily(t, root, "codex")
	if _, _, err := run(t, root, "generate", "codex"); err != nil {
		t.Fatal(err)
	}
	if out, _, err := run(t, root, "generate", "probe"); err != nil || strings.Contains(out, "removed") {
		t.Fatalf("generating one family touched another: %v\n%s", err, out)
	}
}
