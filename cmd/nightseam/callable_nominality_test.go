package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// The native function aliases are assignable by signature; nominal identity
// is selected by the generated exporter and checked at the wire boundary.
func TestGeneratedCallableNominality(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	directory := t.TempDir()
	writeFixture(t, directory, "api/contracts/nominal/model.json", []byte(`{"nightseam":2,"types":{}}`))
	writeFixture(t, directory, "api/contracts/nominal/protocol.json", []byte(`{"profile":"nightseam.duplex/1"}`))
	writeFixture(t, directory, "api/contracts/nominal/live.json", []byte(`{"types":{
		"Report":{"kind":"callable","request":"integer","result":"integer"},
		"SetVolume":{"kind":"callable","request":"integer","result":"integer"}
	}}`))
	if _, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("generate: %v\n%s", err, errs)
	}
	fixtureModule(t, directory, root)
	paths := fixtureTypeScriptPaths(t, directory)
	for _, component := range []string{"runtime", "duplex", "tunnel", "live"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	config, err := json.Marshal(map[string]any{
		"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": paths},
		"include":         []string{"api/ts/**/*.ts", "nominality.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.json", config)
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
	writeFixture(t, directory, "nominality_test.go", []byte(goCallableNominalityFixture))
	writeFixture(t, directory, "nominality.ts", []byte(tsCallableNominalityFixture))
	runFixture(t, directory, "go", "test", "-count=1", "./...")
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "nominality.ts")
}

const goCallableNominalityFixture = `package generated_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	protocol "example.test/generated/api/go/nominal-protocol"
	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/live/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

func TestSameSignatureAssignmentUsesDestinationContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	a, b := duplex.Pipe(8 << 20)
	var scopes [2]*live.Scope
	for i, conn := range []duplex.Conn{a, b} {
		role := runtime.ClientRole
		if i == 1 { role = runtime.ServerRole }
		peer, err := runtime.NewPeer(ctx, conn, role, runtime.Options{
			Prepare: func(p *runtime.Peer) (err error) { scopes[i], err = live.Over(p, live.Options{}); return err },
		})
		if err != nil { t.Fatal(err) }
		defer peer.Close()
	}
	var report protocol.Report = func(_ context.Context, value int64) (int64, error) { return value + 1, nil }
	var volume protocol.SetVolume = report // No conversion or cast: the aliases have the same signature.
	raw, err := protocol.ExportSetVolume(scopes[0].Owner(), volume)
	if err != nil { t.Fatal(err) }
	var descriptor struct { Binding, Contract string }
	if err := json.Unmarshal(raw, &descriptor); err != nil { t.Fatal(err) }
	if descriptor.Binding == "" || descriptor.Contract != "nominal/SetVolume" {
		t.Fatalf("destination exporter wrote %s", raw)
	}
	if err := protocol.ValidateRaw("SetVolume", raw); err != nil { t.Fatal(err) }
	if err := protocol.ValidateRaw("Report", raw); err == nil {
		t.Fatal("validator accepted the destination descriptor as the source contract")
	}
	if _, err := protocol.ImportReport(scopes[1].Owner(), raw); err == nil {
		t.Fatal("import accepted the wrong contract")
	} else {
		var public *runtime.PublicError
		if !errors.As(err, &public) || public.Code != live.ErrorContractMismatch { t.Fatal(err) }
	}
	imported, err := protocol.ImportSetVolume(scopes[1].Owner(), raw)
	if err != nil { t.Fatal(err) }
	if got, err := imported(ctx, 41); err != nil || got != 42 {
		t.Fatalf("assigned implementation answered %d, %v", got, err)
	}
}
`

const tsCallableNominalityFixture = `import * as nominal from './api/ts/nominal-client/src/index.ts';
import * as binding from './api/ts/nominal-binding/src/index.ts';
import { pipe } from '@nightseam/duplex';
import { DuplexError, DuplexPeer, forwardWire } from '@nightseam/runtime';
import { CONTRACT_MISMATCH, liveOver, scopeOf } from '@nightseam/live';

for (const configured of [false, true]) {
const [a, b] = pipe();
const pa = new DuplexPeer({ role: 'client' });
const from = liveOver(pa);
let pb: DuplexPeer | undefined;
try {
  pb = new DuplexPeer({ role: 'server' });
  const to = liveOver(pb, configured ? {maxExports:1, maxImports:1} : {});
  const wire = binding.toWire(() => ({methods:{},events:{}}), {scope:to});
  forwardWire(pb.wire(), wire);
  if (scopeOf(pb) !== to) throw new Error('adapter replaced the host live scope');
  await Promise.all([pa.attach(a), pb.attach(b)]);
  const report: nominal.Report = async value => value + 1;
  const volume: binding.SetVolume = report; // Both roles share the same nominal protocol types.
  const raw = nominal.exportSetVolume(from.owner(), volume);
  const descriptor = raw as { binding: string; contract: string };
  if (!descriptor.binding || descriptor.contract !== 'nominal/SetVolume') {
    throw new Error('destination exporter wrote ' + JSON.stringify(raw));
  }
  nominal.validateWire('SetVolume', raw);
  let refused = false;
  try { nominal.validateWire('Report', raw); } catch { refused = true; }
  if (!refused) throw new Error('validator accepted the destination descriptor as the source contract');
  refused = false;
  try { binding.importReport(to.owner(), raw); }
  catch (error) {
    if (!(error instanceof DuplexError) || error.code !== CONTRACT_MISMATCH) throw error;
    refused = true;
  }
  if (!refused) throw new Error('import accepted the wrong contract');
  const imported = binding.importSetVolume(to.owner(), raw);
  if (await imported(41, { signal: AbortSignal.timeout(5000) }) !== 42) {
    throw new Error('assigned implementation answered wrongly');
  }
} finally {
  pa.close();
  pb?.close();
}
}
`
