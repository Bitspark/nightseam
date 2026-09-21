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
		"SetVolume":{"kind":"callable","request":"integer","result":"integer"},
		"Invocation":{"kind":"record","fields":[{"name":"callback","type":"SetVolume"}]}
	},"server":{"methods":{"accept":{"request":"Invocation","result":"integer"}}},
	"client":{"methods":{"reverse":{"request":"Invocation","result":"integer"}}}}`))
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
	binding "example.test/generated/api/go/nominal-binding"
	client "example.test/generated/api/go/nominal-client"
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

type nominalServer struct { calls *atomic.Int64 }
func (h nominalServer) Accept(ctx context.Context, _ *binding.Remote, params protocol.Invocation) (int64, error) {
	h.calls.Add(1)
	return params.Callback(ctx, 41)
}
type nominalClient struct { calls *atomic.Int64 }
func (h nominalClient) Reverse(ctx context.Context, _ *client.Client, params protocol.Invocation) (int64, error) {
	h.calls.Add(1)
	return params.Callback(ctx, 41)
}

func TestGeneratedGoReverseDispatchChecksTheDigest(t *testing.T) {
	var serverCalls, clientCalls, invoked atomic.Int64
	connected := make(chan *runtime.Peer, 1)
	handler, err := binding.NewHandler(nominalServer{&serverCalls}, runtime.ServerOptions{
		Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil },
		CheckOrigin: func(*http.Request) bool { return true },
		OnConnect: func(peer *runtime.Peer) { connected <- peer },
	})
	if err != nil { t.Fatal(err) }
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	caller, err := client.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), runtime.DialOptions{}, nominalClient{&clientCalls}, client.Events{})
	if err != nil { t.Fatal(err) }
	defer caller.Close()
	var peer *runtime.Peer
	select { case peer = <-connected: case <-ctx.Done(): t.Fatal(ctx.Err()) }
	scope, ok := live.ScopeOf(peer)
	if !ok { t.Fatal("generated server installed no live scope") }
	implementation := func(_ context.Context, n int64) (int64, error) { invoked.Add(1); return n+1, nil }
	old, err := previous.ExportSetVolume(scope.Owner(), implementation)
	if err != nil { t.Fatal(err) }
	var result int64
	err = peer.Call(ctx, "reverse", map[string]json.RawMessage{"callback": old}, &result)
	var mismatch *runtime.PublicError
	if !errors.As(err, &mismatch) || mismatch.Code != "contract_mismatch" || !strings.Contains(mismatch.Message, "nominal/SetVolume") { t.Fatalf("generated reverse refusal: %v", err) }
	if clientCalls.Load() != 0 || invoked.Load() != 0 { t.Fatal("mismatched reference reached the generated reverse handler") }
	current, err := protocol.ExportSetVolume(scope.Owner(), implementation)
	if err != nil { t.Fatal(err) }
	if err := peer.Call(ctx, "reverse", map[string]json.RawMessage{"callback": current}, &result); err != nil || result != 42 { t.Fatalf("matching reverse: %d, %v", result, err) }
	if clientCalls.Load() != 1 || invoked.Load() != 1 { t.Fatal("matching reference did not reach the generated reverse handler once") }
}

func TestGeneratedRevisionsAcrossARealSocket(t *testing.T) {
	for _, mode := range []string{"same", "different"} {
		t.Run(mode, func(t *testing.T) {
			var invoked, dispatched atomic.Int64
			handler, err := binding.NewHandler(nominalServer{&dispatched}, runtime.ServerOptions{
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
					if err := peer.Handle("dispatch_reverse", func(ctx context.Context, _ *runtime.Peer, _ json.RawMessage) (any, error) {
						encode := protocol.ExportSetVolume
						if mode == "different" { encode = previous.ExportSetVolume }
						raw, err := encode(scope.Owner(), func(_ context.Context, n int64) (int64, error) { return n + 1, nil })
						if err != nil { return nil, err }
						var result int64
						err = peer.Call(ctx, "reverse", map[string]json.RawMessage{"callback": raw}, &result)
						return result, err
					}); err != nil { return err }
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
			if got := dispatched.Load(); got != want { t.Fatalf("generated server handler ran %d times, want %d", got, want) }
		})
	}
}
`

const tsCallableDigestSocketFixture = `import assert from 'node:assert/strict';
import * as current from './api/ts/nominal-client/src/types.ts';
import { Client } from './api/ts/nominal-client/src/index.ts';
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
let dispatched = 0;
new Client(peer, { reverse(params) { dispatched++; return params.callback(41); } }, {});
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
  // Use the peer directly so the receiver, rather than the sender's generated
  // preflight validation, must reject the revision before entering its handler.
  const dispatchProtocol = mode === 'same' ? current : previous;
  let dispatchInvoked = 0;
  const dispatchRaw = dispatchProtocol.exportSetVolume(scope.owner(), async value => { dispatchInvoked++; return value + 1; });
  if (mode === 'different') {
    for (const operation of [() => peer.call('accept', { callback: dispatchRaw }), () => peer.call('dispatch_reverse', {})])
      await assert.rejects(operation, (error: unknown) => error instanceof DuplexError && error.code === 'contract_mismatch' && error.message.includes('nominal/SetVolume'));
  } else {
    assert.equal(await peer.call('accept', { callback: dispatchRaw }), 42);
    assert.equal(await peer.call('dispatch_reverse', {}), 42);
  }
  assert.equal(dispatched, mode === 'same' ? 1 : 0);
  assert.equal(dispatchInvoked, mode === 'same' ? 1 : 0);
} finally {
  peer.close();
}
`

const tsCallableNominalityFixture = `import assert from 'node:assert/strict';
import * as nominal from './api/ts/nominal-client/src/index.ts';
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
let serverCalls = 0, reverseCalls = 0;
const handler: binding.Handler = { accept(params) { serverCalls++; return params.callback(41); } };
new nominal.Client(pa, { reverse(params) { reverseCalls++; return params.callback(41); } }, {});
try {
  if (configured) {
    pb = new DuplexPeer({ role: 'server' });
    const existing = liveOver(pb, {maxExports:2, maxImports:1});
    binding.install(pb, handler);
    if (scopeOf(pb) !== existing) throw new Error('install replaced the host live scope');
    await Promise.all([pa.attach(a), pb.attach(b)]);
  } else {
    [pb] = await Promise.all([binding.serve(b, {}, handler), pa.attach(a)]);
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
  const mismatch = (error: unknown): boolean => error instanceof DuplexError && error.code === CONTRACT_MISMATCH && error.message.includes('nominal/SetVolume');
  await assert.rejects(pa.call('accept', { callback: old }), mismatch);
  const reverseOld = previous.exportSetVolume(to.owner(), volume);
  await assert.rejects(pb.call('reverse', { callback: reverseOld }), mismatch);
  assert.equal(serverCalls, 0);
  assert.equal(reverseCalls, 0);
  assert.equal(await pa.call('accept', { callback: raw }), 42);
  const reverseRaw = nominal.exportSetVolume(to.owner(), volume);
  assert.equal(await pb.call('reverse', { callback: reverseRaw }), 42);
  assert.equal(serverCalls, 1);
  assert.equal(reverseCalls, 1);
} finally {
  pa.close();
  pb?.close();
}
}
`
