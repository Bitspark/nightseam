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
	writeFixture(t, directory, "api/contracts/nominal/model.json", []byte(`{"nightseam":2,"types":{"Revision":{"kind":"record","fields":[]}}}`))
	writeFixture(t, directory, "api/contracts/nominal/protocol.json", []byte(`{"profile":"nightseam.duplex/1"}`))
	writeFixture(t, directory, "api/contracts/nominal/live.json", []byte(`{"types":{
		"Report":{"kind":"callable","request":"integer","result":"integer"},
		"SetVolume":{"kind":"callable","request":"integer","result":"integer"}
	}}`))
	if _, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("generate: %v\n%s", err, errs)
	}
	// Keep one generated revision, then add an optional member without
	// changing either callable signature. The same value fits both schemas.
	copyFixtureTree(t, filepath.Join(directory, "api/go/nominal-protocol"), filepath.Join(directory, "api-v1/go/nominal-protocol"))
	copyFixtureTree(t, filepath.Join(directory, "api/ts/nominal-client"), filepath.Join(directory, "api-v1/ts/nominal-client"))
	writeFixture(t, directory, "api/contracts/nominal/model.json", []byte(`{"nightseam":2,"types":{"Revision":{"kind":"record","fields":[{"name":"hint","type":"string","required":false}]}}}`))
	if _, errs, err := run(t, directory, "generate"); err != nil {
		t.Fatalf("generate second revision: %v\n%s", err, errs)
	}
	fixtureModule(t, directory, root)
	paths := fixtureTypeScriptPaths(t, directory)
	for _, component := range []string{"runtime", "duplex", "tunnel", "live"} {
		copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
	}
	config, err := json.Marshal(map[string]any{
		"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": paths, "types": []string{"node"}, "typeRoots": []string{filepath.ToSlash(filepath.Join(root, "node_modules/@types"))}},
		"include":         []string{"api/ts/**/*.ts", "nominality.ts", "nominality-socket.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.json", config)
	writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
	writeFixture(t, directory, "runtime-loader.mjs", []byte(runtimeLoader))
	writeFixture(t, directory, "nominality_test.go", []byte(goCallableNominalityFixture))
	writeFixture(t, directory, "nominality.ts", []byte(tsCallableNominalityFixture))
	writeFixture(t, directory, "nominality-socket.ts", []byte(tsCallableDigestSocketFixture))
	runFixture(t, directory, "go", "test", "-count=1", "./...")
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	runFixture(t, directory, "node", "--loader", "./runtime-loader.mjs", "nominality.ts")
}

const goCallableNominalityFixture = `package generated_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	protocol "example.test/generated/api/go/nominal-protocol"
	previous "example.test/generated/api-v1/go/nominal-protocol"
	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/live/go"
	"github.com/Bitspark/nightseam/runtime/go"
	"github.com/Bitspark/nightseam/tunnel/go"
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
	var descriptor struct { Binding, Contract, Digest string }
	if err := json.Unmarshal(raw, &descriptor); err != nil { t.Fatal(err) }
	if descriptor.Binding == "" || descriptor.Contract != "nominal/SetVolume" {
		t.Fatalf("destination exporter wrote %s", raw)
	}
	if descriptor.Digest != protocol.WireDigest() || previous.WireDigest() == protocol.WireDigest() { t.Fatal("generated revisions did not carry distinct digests") }
	old, err := previous.ExportSetVolume(scopes[0].Owner(), volume)
	if err != nil { t.Fatal(err) }
	for _, check := range []func() error{
		func() error { return protocol.ValidateRaw("SetVolume", old) },
		func() error { _, err := protocol.ImportSetVolume(scopes[1].Owner(), old); return err },
	} {
		var public *runtime.PublicError
		if err := check(); !errors.As(err, &public) || public.Code != live.ErrorContractMismatch { t.Fatalf("same-name different-digest reference: %v", err) }
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

func TestGeneratedRevisionsAcrossARealSocket(t *testing.T) {
	for _, mode := range []string{"same", "different"} {
		t.Run(mode, func(t *testing.T) {
			var invoked atomic.Int64
			handler, err := runtime.NewHandler(runtime.ServerOptions{
				Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil },
				CheckOrigin: func(*http.Request) bool { return true },
				Options: runtime.Options{Prepare: func(peer *runtime.Peer) error {
					carrier, err := tunnel.New(peer, tunnel.Options{Contracts: map[string]string{"nominal": previous.WireDigest()}})
					if err != nil { return err }
					if err := peer.Handle("check_channel", func(ctx context.Context, _ *runtime.Peer, _ json.RawMessage) (any, error) {
						channel, err := carrier.Open(ctx, "nominal", previous.WireDigest())
						if err != nil { return nil, err }
						return nil, channel.Close(ctx, duplex.CodeNormal, "")
					}); err != nil { return err }
					scope, err := live.Over(peer, live.Options{})
					if err != nil { return err }
					if err := peer.Handle("offer", func(context.Context, *runtime.Peer, json.RawMessage) (any, error) {
						return previous.ExportSetVolume(scope.Owner(), func(_ context.Context, n int64) (int64, error) { invoked.Add(1); return n + 1, nil })
					}); err != nil { return err }
					return peer.Handle("check", func(ctx context.Context, _ *runtime.Peer, raw json.RawMessage) (any, error) {
						fn, err := previous.ImportSetVolume(scope.Owner(), raw)
						if err != nil { return nil, err }
						return fn(ctx, 41)
					})
				}},
			})
			if err != nil { t.Fatal(err) }
			server := httptest.NewServer(handler)
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, "node", "--loader", "./runtime-loader.mjs", "nominality-socket.ts", "ws"+strings.TrimPrefix(server.URL, "http"), mode)
			if output, err := command.CombinedOutput(); err != nil { t.Fatalf("generated revisions over socket: %v\n%s", err, output) }
			want := int64(0)
			if mode == "same" { want = 1 }
			if got := invoked.Load(); got != want { t.Fatalf("implementation ran %d times, want %d", got, want) }
		})
	}
}
`

const tsCallableDigestSocketFixture = `import assert from 'node:assert/strict';
import * as current from './api/ts/nominal-client/src/types.ts';
import * as previous from './api-v1/ts/nominal-client/src/types.ts';
import { DuplexPeer, DuplexError } from '@nightseam/runtime';
import { liveOver } from '@nightseam/live';
import { Tunnel } from '@nightseam/tunnel';

const mode = process.argv[3];
const protocol = mode === 'same' ? previous : current;
const peer = new DuplexPeer();
const scope = liveOver(peer);
const carrier = new Tunnel(peer, { contracts: { nominal: protocol.wireDigest } });
let invoked = 0;
try {
  await peer.connect(process.argv[2]!);
  if (mode === 'different') {
    await assert.rejects(carrier.open('nominal', protocol.wireDigest), { code: 'contract_mismatch', message: 'the declaration digest for nominal differs' });
    await assert.rejects(peer.call('check_channel', {}), { code: 'contract_mismatch', message: 'the declaration digest for nominal differs' });
  } else {
    const channel = await carrier.open('nominal', protocol.wireDigest);
    assert.equal(channel.digest, protocol.wireDigest);
    channel.close();
    assert.equal(await peer.call('check_channel', {}), null);
  }
  const offered = await peer.call('offer', {});
  if (mode === 'different') {
    assert.throws(() => protocol.importSetVolume(scope.owner(), offered), (error: unknown) =>
      error instanceof DuplexError && error.code === 'contract_mismatch' && error.message.includes('nominal/SetVolume'));
    assert.equal(scope.counts().imports, 0);
  } else {
    assert.equal(await protocol.importSetVolume(scope.owner(), offered)(41), 42);
  }
  const raw = protocol.exportSetVolume(scope.owner(), async value => { invoked++; return value + 1; });
  if (mode === 'different') {
    await assert.rejects(peer.call('check', raw), (error: unknown) =>
      error instanceof DuplexError && error.code === 'contract_mismatch' && error.message.includes('nominal/SetVolume'));
  } else {
    assert.equal(await peer.call('check', raw), 42);
  }
  assert.equal(invoked, mode === 'same' ? 1 : 0);
} finally {
  peer.close();
}
`

const tsCallableNominalityFixture = `import * as nominal from './api/ts/nominal-client/src/index.ts';
import * as previous from './api-v1/ts/nominal-client/src/types.ts';
import * as binding from './api/ts/nominal-binding/src/index.ts';
import { pipe } from '@nightseam/duplex';
import { DuplexError, DuplexPeer } from '@nightseam/runtime';
import { CONTRACT_MISMATCH, liveOver, scopeOf } from '@nightseam/live';

for (const configured of [false, true]) {
const [a, b] = pipe();
const pa = new DuplexPeer({ role: 'client' });
const from = liveOver(pa);
let pb: DuplexPeer | undefined;
try {
  if (configured) {
    pb = new DuplexPeer({ role: 'server' });
    const existing = liveOver(pb, {maxExports:1, maxImports:1});
    binding.install(pb, {});
    if (scopeOf(pb) !== existing) throw new Error('install replaced the host live scope');
    await Promise.all([pa.attach(a), pb.attach(b)]);
  } else {
    [pb] = await Promise.all([binding.serve(b, {}, {}), pa.attach(a)]);
  }
  const to = scopeOf(pb);
  if (!to) throw new Error('binding installed no live scope');
  const report: nominal.Report = async value => value + 1;
  const volume: binding.SetVolume = report; // Both roles share the same nominal protocol types.
  const raw = nominal.exportSetVolume(from.owner(), volume);
  const descriptor = raw as { binding: string; contract: string; digest: string };
  if (!descriptor.binding || descriptor.contract !== 'nominal/SetVolume') {
    throw new Error('destination exporter wrote ' + JSON.stringify(raw));
  }
  if (descriptor.digest !== nominal.wireDigest || String(previous.wireDigest) === nominal.wireDigest) throw new Error('generated revisions did not carry distinct digests');
  const old = previous.exportSetVolume(from.owner(), volume);
  for (const check of [() => nominal.validateWire('SetVolume', old), () => binding.importSetVolume(to.owner(), old)]) {
    let mismatch = false;
    try { check(); } catch (error) {
      if (!(error instanceof DuplexError) || error.code !== CONTRACT_MISMATCH) throw error;
      mismatch = true;
    }
    if (!mismatch) throw new Error('same-name different-digest reference accepted');
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
