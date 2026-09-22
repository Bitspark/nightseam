package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A carried Envelope does not itself close its generic source family. The
// explicit Go WireType route and TypeScript FamilyBinding retain the same
// closed provenance without deriving identity from a host type or codec.
func TestGeneratedDrawnFamilyIdentity(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	directory := t.TempDir()
	for name, source := range map[string]string{
		"source/model.json":      `{"nightseam":2}`,
		"source/protocol.json":   `{"profile":"nightseam.duplex/1","parameters":[{"name":"T"}],"types":{"Box":{"kind":"record","fields":[{"name":"value","type":"T"}]}},"server":{"methods":{"read":{"result":"Box"}}}}`,
		"consumer/model.json":    `{"nightseam":2}`,
		"consumer/protocol.json": `{"profile":"nightseam.duplex/1","parameters":[{"name":"S","of":"protocol"}],"server":{"methods":{"read":{"result":"S.Envelope"}}}}`,
	} {
		writeFixture(t, directory, "api/contracts/"+name, []byte(source))
	}
	if _, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("generate: %v\n%s", err, errs)
	}
	fixtureModule(t, directory, root)
	writeFixture(t, directory, "drawn_identity_test.go", []byte(goDrawnIdentityFixture))
	runFixture(t, directory, "go", "test", "-count=1", "./...")
	for _, component := range []string{"runtime", "duplex", "tunnel", "live"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	config, err := json.Marshal(map[string]any{
		"compilerOptions": map[string]any{
			"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true,
			"skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true,
			"paths": fixtureTypeScriptPaths(t, directory), "types": []string{"node"},
			"typeRoots": []string{filepath.ToSlash(filepath.Join(root, "node_modules/@types"))},
		},
		"include": []string{"api/ts/**/*.ts", "drawn-identity.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.json", config)
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
	writeFixture(t, directory, "drawn-identity.ts", []byte(tsDrawnIdentityFixture))
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "drawn-identity.ts")
	var exports [2]struct{ Declaration, Digest string }
	for i, name := range []string{"go-drawn-identity.json", "ts-drawn-identity.json"} {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &exports[i]); err != nil {
			t.Fatal(err)
		}
	}
	if exports[0] != exports[1] || exports[0].Declaration == "" || exports[0].Digest == "" {
		t.Fatalf("drawn family provenance differs between Go and TypeScript: %+v", exports)
	}
}

const goDrawnIdentityFixture = `package generated_test
import (
 "context"
 "encoding/json"
 "os"
 "strings"
 "testing"
 "time"
 binding "example.test/generated/api/go/consumer-binding"
 consumer "example.test/generated/api/go/consumer-protocol"
 source "example.test/generated/api/go/source-protocol"

 "github.com/Bitspark/nightseam/runtime/go"
)

// Embedding preserves the generated source family marker and JSON behavior.
// WireType adds the complete interpretation, without inventing a nominal name.
type closedEnvelope struct { source.Envelope }
func (closedEnvelope) WireType() runtime.TypeBinding {
 return runtime.TypeBinding{Schema: source.WireSchema().Bind(map[string]any{"T": runtime.TypeArgument[string]()}, nil), Type: "Envelope"}
}
type methods struct{}
func (methods) Read(context.Context) (closedEnvelope, error) { return closedEnvelope{}, nil }

func TestDrawnFamilyIdentity(t *testing.T) {
 constructions := 0
 _, err := binding.ToWire[source.Envelope](func(consumer.Client[source.Envelope]) (consumer.Server[source.Envelope], error) {
  constructions++; return consumer.Server[source.Envelope]{}, nil
 }, runtime.AdapterContext{}, runtime.JSONAdapter[source.Envelope]())
 if err == nil || !strings.Contains(err.Error(), "missing required binding T") || constructions != 0 {
  t.Fatalf("unbound source interpretation: constructions=%d, err=%v", constructions, err)
 }
 wire, err := binding.ToWire[closedEnvelope](func(consumer.Client[closedEnvelope]) (consumer.Server[closedEnvelope], error) {
  constructions++; return consumer.Server[closedEnvelope]{Methods: methods{}}, nil
 }, runtime.AdapterContext{}, runtime.JSONAdapter[closedEnvelope]())
 if err != nil { t.Fatal(err) }
 defer wire.Close(duplex.CodeNormal, "")
 if constructions != 1 { t.Fatalf("closed interpretation constructed %d models", constructions) }
 drawn := consumer.WireSchema().Bind(map[string]any{"S.Envelope": runtime.TypeArgument[closedEnvelope]()}, nil)
 declaration, err := drawn.BoundDeclaration()
 if err != nil { t.Fatal(err) }
 digest, err := drawn.DeclarationDigest()
 if err != nil { t.Fatal(err) }
 complete := consumer.WireSchema().Bind(nil, map[string]*runtime.Schema{
  "S": source.WireSchema().Bind(map[string]any{"T": runtime.TypeArgument[string]()}, nil),
 })
 expected, err := complete.BoundDeclaration()
 if err != nil || declaration != expected { t.Fatalf("drawn provenance differs from the complete family: %v\n%s\n%s", err, declaration, expected) }
 ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
 defer cancel()
 var advertised runtime.DeclarationIdentity
 if err := runtime.CallWire(ctx, wire, []string{runtime.IdentityMethod}, runtime.DeclarationIdentity{Path: "consumer"}, &advertised); err != nil { t.Fatal(err) }
 if advertised.Path != "consumer" || advertised.Digest != digest { t.Fatalf("generated adapter advertised %+v, want %s", advertised, digest) }
 data, err := json.Marshal(map[string]string{"declaration": declaration, "digest": digest})
 if err != nil { t.Fatal(err) }
 if err := os.WriteFile("go-drawn-identity.json", data, 0600); err != nil { t.Fatal(err) }
}
`

const tsDrawnIdentityFixture = `import assert from 'node:assert/strict';
import { writeFileSync } from 'node:fs';
import { boundDeclaration, declarationDigest, callWire, jsonAdapter, IDENTITY_METHOD, type DeclarationIdentity, type FamilyBinding } from '@nightseam/runtime';
import { toWire, validateWire as consumerValidator } from '@example/consumer-binding';
import { validateWire as sourceValidator, type Family } from '@example/source-client/types';

let constructions = 0;
const model = () => { constructions++; return { methods: { read() { return { version: 1, kind: 'event' }; } }, events: {} }; };
const unbound: FamilyBinding<Family, 'Envelope'> = {
 name: 'source', validate: sourceValidator,
 types: { Envelope: jsonAdapter<Family['Envelope']>({ validate: sourceValidator, type: 'Envelope' }) },
};
assert.throws(() => toWire(model, {}, unbound), /missing required binding T/);
assert.equal(constructions, 0, 'an unbound family constructed its model');
const closedSlots = { T: { validate: sourceValidator, type: 'string' } };
const closed: FamilyBinding<Family, 'Envelope'> = {
 name: 'source', validate: sourceValidator,
 slots: closedSlots,
 types: { Envelope: jsonAdapter<Family['Envelope']>({ validate: sourceValidator, type: 'Envelope', slots: closedSlots }) },
};
const wire = toWire(model, {}, closed);
try {
 assert.equal(constructions, 1);
 const declaration = boundDeclaration(consumerValidator, { S: closed });
 const digest = declarationDigest(consumerValidator, { S: closed });
 const advertised = await callWire<DeclarationIdentity>(wire, [IDENTITY_METHOD], { path: 'consumer' }, { timeoutMs: 5000 });
 assert.deepEqual(advertised, { path: 'consumer', digest });
 writeFileSync('ts-drawn-identity.json', JSON.stringify({ declaration, digest }));
} finally { wire.close(); }
`
