package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/model"
)

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

// TestTiersAreModelProtocolAndLive: the tier table is exactly the three
// levels v0.5.0 is — self-contained data, ordinary RPC, live values — in
// that order, with the live tier bringing no built-in family of its own.
// A checkout that carries a session.json is still refused with a
// diagnostic naming the file: the governed session tier is gone, and the
// live tier replaced it rather than being it renamed.
func TestTiersAreModelProtocolAndLive(t *testing.T) {
	var names []string
	for _, tier := range model.Tiers {
		names = append(names, tier.Name+"="+tier.File)
	}
	if strings.Join(names, ",") != "model=model.json,protocol=protocol.json,live=live.json" {
		t.Fatalf("the tiers are %v", names)
	}
	if roles := model.TierRoles(); strings.Join(roles, ",") != "protocol,live" {
		t.Fatalf("a family parameter may be of %v", roles)
	}
	if model.IsTierRole("session") {
		t.Fatal("session is still a tier a family parameter may be of")
	}
	for _, tier := range model.Tiers {
		if tier.File == model.LiveFile && tier.Builtin != "" {
			t.Fatalf("the live tier brings the built-in %s; the wire form of a live value is the projection of the callable kind, not a declared type a family carries", tier.Builtin)
		}
	}
	root := t.TempDir()
	writeFamily(t, root, "probe")
	writeFixture(t, root, "api/contracts/probe/session.json", []byte(`{"decides": ["echo"]}`))
	_, errs, err := run(t, root, "validate")
	if err == nil || !strings.Contains(errs, "probe/session.json#:") || !strings.Contains(errs, "[unknown_file]") {
		t.Fatalf("a session tier file was not refused: %v\n%s", err, errs)
	}
}

// TestLiveTierStacksOnTheProtocol: live.json needs protocol.json beside
// it, so that the levels stay a stack from the bottom and a live
// declaration always has an RPC surface to add operations to.
func TestLiveTierStacksOnTheProtocol(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "api/contracts/x/model.json", []byte(`{"nightseam": 2, "types": {}}`))
	writeFixture(t, root, "api/contracts/x/live.json", []byte(`{"types": {}}`))
	_, errs, err := run(t, root, "validate")
	if err == nil || !strings.Contains(errs, "[missing_tier]") || !strings.Contains(errs, "protocol.json") {
		t.Fatalf("a live tier without a protocol tier was not refused: %v\n%s", err, errs)
	}
}

// TestTierFilesAreLoadedAndRendered: a family declared in tier files
// validates and renders as one.
func TestTierFilesAreLoadedAndRendered(t *testing.T) {
	root := t.TempDir()
	writeFamily(t, root, "probe")
	if out, _, err := run(t, root, "validate"); err != nil || out != "1 families; valid\n" {
		t.Fatalf("validate: %v\n%s", err, out)
	}
	if out, _, err := run(t, root, "generate"); err != nil || strings.Count(out, "generated ") != 13 {
		t.Fatalf("generate: %v\n%s", err, out)
	}
	client, err := os.ReadFile(filepath.Join(root, "api/go/probe-client/client_generated.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"type Caller interface", "var _ Caller = (*Client)(nil)", "func (c *Client) Echo("} {
		if !strings.Contains(string(client), want) {
			t.Errorf("the Go client lacks %s", want)
		}
	}
	for _, absent := range []string{"func Decides(", "func Asks(", "var Conversation ="} {
		if strings.Contains(string(client), absent) {
			t.Errorf("the Go client still declares %s", absent)
		}
	}
	index, err := os.ReadFile(filepath.Join(root, "api/ts/probe-client/src/index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"export interface Caller {", "export class Client implements Caller {"} {
		if !strings.Contains(string(index), want) {
			t.Errorf("the TypeScript client lacks %s", want)
		}
	}
	for _, absent := range []string{"export const decides", "export const asks", "export const conversation"} {
		if strings.Contains(string(index), absent) {
			t.Errorf("the TypeScript client still declares %s", absent)
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
	writeFixture(t, root, "api/contracts/probe.rpc.json", []byte(`{}`))
	_, errs, err := run(t, root, "validate")
	if err == nil || !strings.Contains(errs, "probe/probe.rpc.json#: probe.rpc.json is a file; a family is a directory of tier files, api/contracts/probe/. [stray_file]") {
		t.Fatalf("a file where a family should be was not named: %v\n%s", err, errs)
	}
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

// TestParametersAreLoadedFromTheProtocolTier: a family generic in another
// family renders its types with the parameter's uses as Go type
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

// A model-only family has generated types but no protocol handler to implement.
func TestInitSkipsModelOnlyFamilies(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "api/contracts/catalog/model.json", []byte(`{"nightseam":2,"types":{"Item":{"kind":"record","fields":[]}}}`))
	if out, errs, err := run(t, root, "generate", "catalog"); err != nil {
		t.Fatalf("generate: %v\n%s%s", err, out, errs)
	}
	if _, err := os.Stat(filepath.Join(root, "api/go/catalog-protocol/types_generated.go")); err != nil {
		t.Fatalf("the model's generated types are missing: %v", err)
	}
	if out, errs, err := run(t, root, "init", "catalog"); err != nil || out != "" || errs != "" {
		t.Fatalf("model-only init must write no handlers: %v\n%s%s", err, out, errs)
	}
	if _, err := os.Stat(filepath.Join(root, "api/impl/catalog")); !os.IsNotExist(err) {
		t.Fatalf("model-only init created an implementation directory: %v", err)
	}
	if out, errs, err := run(t, root, "check", "catalog"); err != nil || out != "" || errs != "" {
		t.Fatalf("init disturbed generated output: %v\n%s%s", err, out, errs)
	}
}

// TestInitWritesTheHandlersOnce: init writes a Go server handler and a
// TypeScript client handler for a family into a directory of the
// consumer's own, compiling against the generated packages, and leaves
// a file that exists as it is.
func TestInitWritesTheHandlersOnce(t *testing.T) {
	root := t.TempDir()
	writeFamily(t, root, "probe")
	out, _, err := run(t, root, "init", "probe")
	if err != nil || !strings.Contains(out, "wrote api/impl/probe/handler.go") || !strings.Contains(out, "wrote api/impl/probe/handler.ts") {
		t.Fatalf("init: %v\n%s", err, out)
	}
	handler, err := os.ReadFile(filepath.Join(root, "api/impl/probe/handler.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"package probe", "var _ protocol.ServerMethods = Handler{}", "func (Handler) Echo(ctx context.Context, params protocol.Payload) (protocol.Payload, error)", `Code: "unimplemented"`} {
		if !strings.Contains(string(handler), want) {
			t.Errorf("the Go handler lacks %s:\n%s", want, handler)
		}
	}
	stub, err := os.ReadFile(filepath.Join(root, "api/impl/probe/handler.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stub), "export const model: ClientModel = remote => ({") || !strings.Contains(string(stub), "async reverse(params, context)") || !strings.Contains(string(stub), `throw new Error("reverse is not implemented")`) {
		t.Errorf("the TypeScript handler is wrong:\n%s", stub)
	}
	writeFixture(t, root, "api/impl/probe/handler.go", []byte("package probe // mine\n"))
	out, _, err = run(t, root, "init", "probe")
	if err != nil || !strings.Contains(out, "kept api/impl/probe/handler.go") {
		t.Fatalf("init rewrote: %v\n%s", err, out)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "api/impl/probe/handler.go")); string(data) != "package probe // mine\n" {
		t.Fatal("init rewrote the consumer's file")
	}
	// A generic family's handler is generic in the same parameters.
	writeFamily(t, root, "carrier")
	if _, _, err := run(t, root, "init", "carrier", "--dir", "impl/{family}"); err != nil {
		t.Fatal(err)
	}
	generic, _ := os.ReadFile(filepath.Join(root, "impl/carrier/handler.go"))
	if !strings.Contains(string(generic), "type Handler[SEnvelope, SHandle any] struct{}") || !strings.Contains(string(generic), "func (Handler[SEnvelope, SHandle]) Relay(ctx context.Context, params protocol.Frame[SEnvelope]) (probeprotocol.Envelope, error)") {
		t.Errorf("the generic handler is wrong:\n%s", generic)
	}
}
