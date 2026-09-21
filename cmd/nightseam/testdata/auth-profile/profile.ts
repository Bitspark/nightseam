// The TypeScript side of the authority profile over generated sockets. As
// the client of a Go server it runs the real exchange with bob's key, is
// decided by the Go guard, holds returned callables through expiry, and
// decides the reverse direction under the context it gives the server by
// construction. As the server of a Go client it serves the guarded model
// under its own layer and is where every decision is made.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import * as binding from '@example/worker-binding';
import type { Client, Job, Progress, ProgressSink, Server } from '@example/worker-client/types';
import { wireDeclaration, wireDigest } from '@example/worker-client/types';
import { DuplexError, DuplexPeer, forwardWire } from '@nightseam/runtime';
import { liveOver, valueEnvironment, type LiveOwner } from '@nightseam/live';
import { audience, prove, type Context } from '@nightseam/auth/connection';
import { bind, type Policy } from '@nightseam/auth/exposure';
import type { Root, Time } from '@nightseam/auth/grant';
import { authOver, guard, payloadOf, surfaceOf } from '@nightseam/auth/over';

const url = process.argv[2]!;
const role = process.argv[3]!;
const options = () => ({ signal: AbortSignal.timeout(8000) });

// ---- the tables' keys ---------------------------------------------------------------------

const table = JSON.parse(readFileSync('auth-exposure.json', 'utf8')) as {
  root: { key: string; domain: string };
  keys: Record<string, { seed: string; pubkey: string }>;
  envelopes: Record<string, string>;
};
const hex = (text: string): Uint8Array => Uint8Array.from(Buffer.from(text, 'hex'));
const toHex = (bytes: Uint8Array): string => Buffer.from(bytes).toString('hex');
const key = (name: string): Uint8Array => hex(table.keys[name]!.pubkey);
const seed = (name: string): Uint8Array => hex(table.keys[name]!.seed);
const envelope = (name: string): Uint8Array => hex(table.envelopes[name]!);
const root: Root = { key: key(table.root.key), domain: table.root.domain };

let clock = 1799990000n;
const now = (): Time => ({ present: true, now: clock });
let nonces = 0;
const nonce = (): Uint8Array => new Uint8Array(32).fill(0xa0 + ++nonces);

async function refused(what: string, run: () => Promise<unknown>, code: string): Promise<DuplexError> {
  let error: unknown;
  try {
    await run();
  } catch (e) {
    error = e;
  }
  assert(error instanceof DuplexError, `${what}: no refusal, wanted ${code}`);
  assert.equal(error.code, code, `${what}: ${error.code} (${error.message})`);
  return error;
}

// ---- the client of a Go server -------------------------------------------------------------------

async function goServer(): Promise<void> {
  // The client's own exposure — its `notify` and the sink it supplies —
  // decided under the context it gives the server it dialed, by
  // construction: the service key, whose grant is `report` over projects.
  const service: Context = { subject: key('service'), chain: [envelope('root_service')], validity: { finite: false }, since: 0n };
  const clientPolicy: Policy = {
    family: 'worker',
    digest: wireDigest,
    treatments: {
      'method:notify': { kind: 'guarded', action: 'report', scope: 'projects/{projectId}' },
      'callable:JobStatus': { kind: 'denied' },
      'callable:JobCancel': { kind: 'denied' },
      'callable:ProgressSink': { kind: 'guarded', action: 'report', scope: '{export}' },
    },
  };
  const bound = bind(surfaceOf('worker', 'client', wireDeclaration, wireDigest), clientPolicy);
  assert(!('code' in bound), JSON.stringify(bound));
  const g = guard(bound, { root, now });
  let notified = 0;
  const received: Progress[] = [];
  let scope!: ReturnType<typeof liveOver>;
  const peer = new DuplexPeer({ prepare: (p) => { scope = liveOver(p, { maxImports: 32, maxExports: 64 }); } });
  authOver(peer, { root, now, nonce, trusted: service });
  await peer.connect(url);
  const model = await binding.fromWire(peer.wire(), { valueEnvironment: valueEnvironment(scope) });
  const access = model({
    methods: {
      notify(params, context) {
        g.decide(context, 'method:notify', payloadOf(params));
        notified++;
        return true;
      },
    },
    events: { progress: (data) => void received.push(data) },
  } as Client);
  try {
    // AUTH-SEAM-001, real: nothing protected before the exchange.
    await refused('before the exchange', () => Promise.resolve(access.methods.list({ projectId: '7' }, options())), 'auth.unauthenticated');
    assert.equal(await access.methods.whoami({}, options()), '');
    // The exchange, with bob's key, over the audience the client reached.
    const challenged = await peer.call<{ nonce: string }>('auth.challenge', {}, options());
    const proof = prove(seed('bob'), audience(url), hex(challenged.nonce));
    const proved = await peer.call<{ expires_at?: number }>(
      'auth.prove',
      { subject: 'ed25519:' + toHex(key('bob')), possession: toHex(proof), chain: [envelope('root_alice'), envelope('alice_bob')].map(toHex) },
      options(),
    );
    assert.deepEqual(proved, { expires_at: 1800000000 });
    assert.equal(await access.methods.whoami({}, options()), toHex(key('bob')));
    await refused('a context is immutable', () => peer.call('auth.challenge', {}, options()), 'auth.established');
    // AUTH-SEAM-002, real: decided on the chain.
    assert.deepEqual(await access.methods.list({ projectId: '7' }, options()), { count: 1 });
    const denied = await refused('sibling', () => Promise.resolve(access.methods.list({ projectId: '8' }, options())), 'auth.denied');
    assert.deepEqual(denied.data, { code: 'not_covered', hop: 1 });
    await refused('prefix', () => Promise.resolve(access.methods.list({ projectId: '70' }, options())), 'auth.denied');
    await refused('selector', () => Promise.resolve(access.methods.list({ projectId: '7/ledger' }, options())), 'auth.selector_invalid');
    // AUTH-SEAM-003, real: returned callables decided at each invocation; a supplied sink decided here.
    const owner: LiveOwner = scope.owner().child();
    const job: Job = await access.methods.start({ projectId: '7' }, { ...options(), valueContext: owner });
    assert.equal(await job.status('j1', options()), 'running:j1');
    assert.equal(await job.cancel('j1', options()), true);
    let reported = 0;
    const sink: ProgressSink = async (p, context) => {
      g.decide(context, 'callable:ProgressSink', payloadOf(p));
      reported++;
      return true;
    };
    // The sink is refused where the server's context does not cover the export... which it does: report over projects.
    g.export('sink', 'callable:ProgressSink', 'projects/7');
    await refused('subscribe on a project bob may not read', () => Promise.resolve(access.methods.subscribe({ projectId: '8', sink }, { ...options(), valueContext: owner })), 'auth.denied');
    // The reverse direction: the server calls notify; decided here under the service's context.
    assert.deepEqual(await peer.call('test.notify', { projectId: '7', percent: 2 }, options()), { ok: true });
    assert.equal(notified, 1);
    assert.deepEqual(await peer.call('test.notify', { projectId: 'x/y', percent: 2 }, options()), { refused: 'auth.selector_invalid' });
    // AUTH-SEAM-004, real: an emission decided per recipient.
    assert.deepEqual(await peer.call('test.emit', { projectId: '7', percent: 50 }, options()), { delivered: 1, dropped: 0 });
    const deadline = Date.now() + 5000;
    while (received.length < 1) {
      assert(Date.now() < deadline, 'emission not received');
      await new Promise((r) => setTimeout(r, 2));
    }
    assert.deepEqual(await peer.call('test.emit', { projectId: '8', percent: 50 }, options()), { delivered: 0, dropped: 1 });
    // AUTH-SEAM-005, real: expiry at use with the socket open; the retained callable refused; the clock returns.
    await peer.call('test.clock', 1800000000, options());
    const expired = await refused('expired at use', () => Promise.resolve(access.methods.list({ projectId: '7' }, options())), 'auth.denied');
    assert.deepEqual(expired.data, { code: 'expired', hop: 1 });
    await refused('retained callable after expiry', () => job.status('j1', options()), 'auth.denied');
    assert.deepEqual(await peer.call('test.emit', { projectId: '7', percent: 60 }, options()), { delivered: 0, dropped: 1 });
    await peer.call('test.clock', 1799990000, options());
    assert.equal(await job.status('j2', options()), 'running:j2');
    owner.release();
    assert.equal(reported, 0);
    console.log('AUTH-PROFILE typescript client: exchanged, decided, emitted once, expired at use, recovered');
  } finally {
    peer.close();
  }
}

// ---- the server of a Go client -------------------------------------------------------------------

async function typescriptServer(): Promise<void> {
  const serverPolicy: Policy = {
    family: 'worker',
    digest: wireDigest,
    treatments: {
      'method:list': { kind: 'guarded', action: 'read', scope: 'projects/{projectId}' },
      'method:whoami': { kind: 'public' },
      'method:start': { kind: 'guarded', action: 'write', scope: 'projects/{projectId}' },
      'method:subscribe': { kind: 'guarded', action: 'read', scope: 'projects/{projectId}' },
      'event:progress': { kind: 'guarded', action: 'read', scope: 'projects/{projectId}' },
      'callable:JobStatus': { kind: 'guarded', action: 'read', scope: '{export}' },
      'callable:JobCancel': { kind: 'guarded', action: 'write', scope: '{export}' },
      'callable:ProgressSink': { kind: 'denied' },
    },
  };
  const bound = bind(surfaceOf('worker', 'server', wireDeclaration, wireDigest), serverPolicy);
  assert(!('code' in bound), JSON.stringify(bound));
  const g = guard(bound, { root, now });
  let jobs = 0;
  const model = (): Server => ({
    methods: {
      list(params, context) {
        g.decide(context, 'method:list', payloadOf(params));
        return { count: 1 };
      },
      whoami(_params, context) {
        g.decide(context, 'method:whoami', {});
        return '';
      },
      start(params, context) {
        const decision = g.decide(context, 'method:start', payloadOf(params));
        g.effect(context, decision);
        const ref = `job:${params.projectId}:${++jobs}`;
        g.export(ref, 'callable:JobStatus', 'projects/' + params.projectId);
        g.export(ref + ':cancel', 'callable:JobCancel', 'projects/' + params.projectId);
        return {
          projectId: params.projectId,
          status: async (id, context) => {
            g.invoke(context, ref);
            return 'running:' + id;
          },
          cancel: async (_id, context) => {
            g.invoke(context, ref + ':cancel');
            return true;
          },
        };
      },
      async subscribe(params, context) {
        g.decide(context, 'method:subscribe', payloadOf(params));
        return params.sink({ projectId: params.projectId, percent: 1 });
      },
    },
    events: {},
  });
  // The model is bound and forwarded in prepare — which the peer runs as it
  // is built, before it reads — as a Go server does, so that the other
  // side's first request finds it.
  let detach = () => {};
  let wire: ReturnType<typeof binding.toWire> | undefined;
  const peer = new DuplexPeer({
    prepare: (p) => {
      const scope = liveOver(p, { maxImports: 32, maxExports: 64 });
      wire = binding.toWire(model, { valueEnvironment: valueEnvironment(scope) });
      detach = forwardWire(p.wire(), wire);
    },
  });
  authOver(peer, { root, audience: audience(url), now, nonce });
  let finish!: () => void;
  const done = new Promise<void>((resolve) => { finish = resolve; });
  peer.handle('test.clock', (at) => { clock = BigInt(at as number); return at; });
  // The answer leaves before the peer closes.
  peer.handle('test.done', () => { setTimeout(finish, 50); return null; });
  await peer.connect(url);
  try {
    await done;
    console.log('AUTH-PROFILE typescript server: served under its own layer');
  } finally {
    detach();
    wire?.close();
    peer.close();
  }
}

if (role === 'go-server') await goServer();
else if (role === 'typescript-server') await typescriptServer();
else throw new Error(`unknown role ${role}`);
