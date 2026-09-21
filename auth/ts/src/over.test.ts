/**
 * The profile over a peer: the exchange installed beside a model's handlers,
 * the context reachable from a handler through the peer, and the guard that
 * decides a member from a handler's own context — over a pipe of two peers,
 * with the keys and envelopes the tables share, so that what the exchange
 * establishes here is what `auth-boot.json` says it establishes.
 */
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { pipe } from '@nightseam/duplex';
import { DuplexError, DuplexPeer } from '@nightseam/runtime';
import { fromHex, toHex } from './bytes.ts';
import { prove } from './connection.ts';
import { bind, type Policy, type Surface } from './exposure.ts';
import * as grant from './grant.ts';
import { authOf, authOver, contextOf, guard, payloadOf, surfaceOf, type AuthLayer } from './over.ts';

const table = JSON.parse(
  readFileSync(path.join(import.meta.dirname, '..', '..', '..', 'conformance', 'tables', 'auth-exposure.json'), 'utf8'),
) as {
  root: { key: string; domain: string };
  keys: Record<string, { seed: string; pubkey: string }>;
  envelopes: Record<string, string>;
};
const hex = (text: string): Uint8Array => fromHex(text)!;
const key = (name: string): Uint8Array => hex(table.keys[name]!.pubkey);
const seed = (name: string): Uint8Array => hex(table.keys[name]!.seed);
const envelope = (name: string): Uint8Array => hex(table.envelopes[name]!);
const root: grant.Root = { key: key(table.root.key), domain: table.root.domain };
const audience = 'https://api.example.test/nightseam';
const options = () => ({ signal: AbortSignal.timeout(5000) });

/** A minimal declaration, in the generator's rendering, for one method and one event. */
const declaration = JSON.stringify({
  definitions: {
    worker: {
      kind: 'family',
      server: { ref: 'worker/$server' },
      client: { ref: 'worker/$client' },
      types: { Status: { ref: 'worker/Status' } },
    },
    'worker/$server': {
      kind: 'side',
      methods: { list: { request: { ref: 'worker/ListRequest' } } },
      events: { progress: { ref: 'worker/Progress' } },
    },
    'worker/$client': { kind: 'side', methods: {}, events: {} },
    'worker/ListRequest': { kind: 'record', fields: [{ name: 'projectId' }] },
    'worker/Progress': { kind: 'record', fields: [{ name: 'projectId' }, { name: 'percent' }] },
    'worker/Status': { kind: 'callable', request: { primitive: 'string' } },
  },
});
const surface: Surface = surfaceOf('worker', 'server', declaration, 'd1');
const policy: Policy = {
  family: 'worker',
  digest: 'd1',
  treatments: {
    'method:list': { kind: 'guarded', action: 'read', scope: 'projects/{projectId}' },
    'event:progress': { kind: 'guarded', action: 'read', scope: 'projects/{projectId}' },
    'callable:Status': { kind: 'guarded', action: 'read', scope: '{export}' },
  },
};

let clock = 1799990000n;
const now = (): grant.Time => ({ present: true, now: clock });

async function paired(server: (peer: DuplexPeer) => AuthLayer | undefined) {
  const [a, b] = pipe();
  const client = new DuplexPeer({ role: 'client' });
  const serving = new DuplexPeer({ role: 'server' });
  const layer = server(serving);
  await Promise.all([client.attach(a), serving.attach(b)]);
  return { client, serving, layer };
}

async function refused(run: () => Promise<unknown>, code: string): Promise<DuplexError> {
  let error: unknown;
  try {
    await run();
  } catch (e) {
    error = e;
  }
  assert(error instanceof DuplexError, `no refusal, wanted ${code}`);
  assert.equal(error.code, code);
  return error;
}

test("the surface is read off the declaration, sorted, with each member's request fields", () => {
  assert.deepEqual(surface, {
    family: 'worker',
    digest: 'd1',
    members: [
      { key: 'callable:Status', fields: [] },
      { key: 'event:progress', fields: ['percent', 'projectId'] },
      { key: 'method:list', fields: ['projectId'] },
    ],
  });
  assert.throws(() => surfaceOf('nobody', 'server', declaration, 'd1'), { code: 'auth.contract_mismatch' });
});

test('the exchange establishes the context the table says, and a handler decides on it', async (t) => {
  clock = 1799990000n;
  const bound = bind(surface, policy);
  assert(!('code' in bound));
  const g = guard(bound, { root, now });
  let ran = 0;
  const { client, serving, layer } = await paired((peer) => {
    const layer = authOver(peer, { root, audience, now, nonce: () => hex('a1'.repeat(32)) });
    peer.handle('list', (params, context) => {
      const decision = g.decide(context, 'method:list', payloadOf(params));
      ran++;
      g.effect(context, decision);
      return { count: 1, subject: toHex(decision.subject!) };
    });
    return layer;
  });
  t.after(() => {
    client.close();
    serving.close();
  });
  assert.equal(authOf(serving), layer);
  assert.equal(layer!.context(), undefined);

  // Before the exchange: a protected member refuses, and no body ran.
  await refused(() => client.call('list', { projectId: '7' }, options()), 'auth.unauthenticated');
  assert.equal(ran, 0);

  // The exchange, as AUTH-BOOT-016: bob proves over the challenge and presents his chain.
  const challenged = await client.call<{ nonce: string }>('auth.challenge', {}, options());
  assert.equal(challenged.nonce, 'a1'.repeat(32));
  const proof = prove(seed('bob'), audience, hex(challenged.nonce));
  const proved = await client.call<{ expires_at?: number }>(
    'auth.prove',
    {
      subject: 'ed25519:' + toHex(key('bob')),
      possession: toHex(proof),
      chain: [envelope('root_alice'), envelope('alice_bob')].map(toHex),
    },
    options(),
  );
  assert.deepEqual(proved, { expires_at: 1800000000 });
  assert.equal(toHex(layer!.context()!.subject), toHex(key('bob')));

  // A context is immutable.
  await refused(() => client.call('auth.challenge', {}, options()), 'auth.established');
  await refused(
    () =>
      client.call(
        'auth.prove',
        { subject: 'ed25519:' + toHex(key('alice')), possession: toHex(proof), chain: [] },
        options(),
      ),
    'auth.established',
  );

  // Decided on the connection's context: own project admitted, a sibling refused with the grant's code and hop.
  assert.deepEqual(await client.call('list', { projectId: '7' }, options()), { count: 1, subject: toHex(key('bob')) });
  assert.equal(ran, 1);
  const denied = await refused(() => client.call('list', { projectId: '8' }, options()), 'auth.denied');
  assert.deepEqual(denied.data, { grant: { code: 'not_covered', hop: 1 } });
  await refused(() => client.call('list', { projectId: '7/ledger' }, options()), 'auth.selector_invalid');
  assert.equal(ran, 1);

  // Expiry at use, with the connection open; then the clock returns.
  clock = 1800000000n;
  const expired = await refused(() => client.call('list', { projectId: '7' }, options()), 'auth.denied');
  assert.deepEqual(expired.data, { grant: { code: 'expired', hop: 1 } });
  clock = 1799990000n;
  assert.deepEqual(await client.call('list', { projectId: '7' }, options()), { count: 1, subject: toHex(key('bob')) });
});

test('a failed prove consumes the challenge; a malformed prove is an attempt too; no audience refuses the exchange', async (t) => {
  clock = 1799990000n;
  const { client, serving } = await paired((peer) =>
    authOver(peer, { root, audience, now, nonce: () => hex('a1'.repeat(32)) }),
  );
  t.after(() => {
    client.close();
    serving.close();
  });
  await refused(
    () =>
      client.call(
        'auth.prove',
        { subject: 'ed25519:' + toHex(key('bob')), possession: '00'.repeat(64), chain: [] },
        options(),
      ),
    'auth.no_challenge',
  );
  await client.call('auth.challenge', {}, options());
  await refused(() => client.call('auth.prove', { subject: 'not a key' }, options()), 'auth.malformed');
  await refused(
    () =>
      client.call(
        'auth.prove',
        { subject: 'ed25519:' + toHex(key('bob')), possession: '00'.repeat(64), chain: [] },
        options(),
      ),
    'auth.no_challenge',
  );
  await client.call('auth.challenge', {}, options());
  const wrong = prove(seed('mallory'), audience, hex('a1'.repeat(32)));
  await refused(
    () =>
      client.call(
        'auth.prove',
        {
          subject: 'ed25519:' + toHex(key('bob')),
          possession: toHex(wrong),
          chain: [envelope('root_alice'), envelope('alice_bob')].map(toHex),
        },
        options(),
      ),
    'auth.possession_invalid',
  );

  const piped = await paired((peer) => authOver(peer, { root, now, nonce: () => hex('a1'.repeat(32)) }));
  t.after(() => {
    piped.client.close();
    piped.serving.close();
  });
  await refused(() => piped.client.call('auth.challenge', {}, options()), 'auth.unsupported');
});

test('a context given by construction is trust: the exchange is refused, the guard decides on it', async (t) => {
  clock = 1799990000n;
  const bound = bind(surface, policy);
  assert(!('code' in bound));
  const g = guard(bound, { root, now });
  const trusted = {
    subject: key('alice'),
    chain: [envelope('root_alice')],
    validity: { finite: false } as grant.Validity,
    since: 0n,
  };
  const { client, serving } = await paired((peer) => {
    const layer = authOver(peer, { root, now, nonce: () => hex('a1'.repeat(32)), trusted });
    peer.handle('list', (params, context) => {
      assert.equal(contextOf(context), trusted);
      return toHex(g.decide(context, 'method:list', payloadOf(params)).subject!);
    });
    return layer;
  });
  t.after(() => {
    client.close();
    serving.close();
  });
  await refused(() => client.call('auth.challenge', {}, options()), 'auth.established');
  assert.equal(await client.call('list', { projectId: '9' }, options()), toHex(key('alice')));
  assert.equal(contextOf(undefined), undefined);
  assert.equal(contextOf({ peer: client }), undefined);
});

test('an exported reference is decided by its record under the invoking connection; an emission per recipient', async (t) => {
  clock = 1799990000n;
  const bound = bind(surface, policy);
  assert(!('code' in bound));
  const g = guard(bound, { root, now });
  g.export('status:7', 'callable:Status', 'projects/7');
  assert.throws(() => g.export('x', 'method:list', 'projects/7'), { code: 'auth.unknown_member' });
  const bob = await paired((peer) => authOver(peer, { root, audience, now, nonce: () => hex('a1'.repeat(32)) }));
  const carol = await paired((peer) => authOver(peer, { root, audience, now, nonce: () => hex('a1'.repeat(32)) }));
  t.after(() => {
    for (const p of [bob, carol]) {
      p.client.close();
      p.serving.close();
    }
  });
  for (const [who, p, chain] of [
    ['bob', bob, ['root_alice', 'alice_bob']],
    ['carol', carol, ['root_alice', 'alice_bob', 'bob_carol']],
  ] as const) {
    const { nonce } = await p.client.call<{ nonce: string }>('auth.challenge', {}, options());
    await p.client.call(
      'auth.prove',
      {
        subject: 'ed25519:' + toHex(key(who)),
        possession: toHex(prove(seed(who), audience, hex(nonce))),
        chain: chain.map(envelope).map(toHex),
      },
      options(),
    );
  }
  // The same reference, invoked under each connection: bob's context covers projects/7, carol's does not.
  assert.equal(g.invoke({ peer: bob.serving }, 'status:7').scope, 'projects/7');
  await refused(async () => g.invoke({ peer: carol.serving }, 'status:7'), 'auth.denied');
  await refused(async () => g.invoke(undefined, 'status:7'), 'auth.unauthenticated');
  await refused(async () => g.invoke({ peer: bob.serving }, 'nobody'), 'auth.reference_unknown');
  // One emission, decided per recipient.
  const deliveries = g.emit('event:progress', { projectId: '7', percent: 50 }, [
    { name: 'bob', peer: bob.serving },
    { name: 'carol', peer: carol.serving },
    { name: 'nobody', peer: bob.client },
  ]);
  assert.deepEqual(
    deliveries.map((d) => [d.recipient, d.refused?.code]),
    [
      ['bob', undefined],
      ['carol', 'auth.denied'],
      ['nobody', 'auth.unauthenticated'],
    ],
  );
});
