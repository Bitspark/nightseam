/**
 * The grant held to conformance/tables/auth-grant.json: every case of every
 * family, its exact bytes, result or code and hop. The table is loaded as
 * data; nothing here knows how it was produced.
 */
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { getPublicKey } from '@bitspark/archon';
import { fromHex, toHex } from './bytes.ts';
import * as grant from './grant.ts';

const table = JSON.parse(
  readFileSync(path.join(import.meta.dirname, '..', '..', '..', 'conformance', 'tables', 'auth-grant.json'), 'utf8'),
) as Table;

interface Table {
  domain: string;
  keys: Record<string, { seed: string; pubkey: string }>;
  envelopes: Record<string, string>;
  cases: Case[];
}
interface JsonGrant {
  domain: string;
  subject: string;
  parent: string | null;
  scope: string[] | null;
  actions: string[] | null;
  delegable: string[] | null;
  depth: number;
  validity: 'unbounded' | 'inherit' | { expires_at: number };
}
interface Case {
  family: string;
  id: string;
  name: string;
  grant?: JsonGrant;
  body?: string;
  refused?: unknown;
  issuer?: string;
  envelope?: string;
  digest?: string;
  root?: { key: string; domain: string };
  chain?: string[];
  request?: grant.Request;
  now?: number | null;
  verified?: { subject: string; depth: number; validity: unknown; hops: number };
  parents?: string[] | null;
  child?: JsonGrant;
  same_as?: string;
}

const hex = (text: string): Uint8Array => {
  const bytes = fromHex(text);
  assert(bytes !== undefined, `not hex: ${text}`);
  return bytes;
};
/** A key by name or a 32-byte hex, as the table spells subjects and parents. */
const key = (name: string): Uint8Array => (table.keys[name] ? hex(table.keys[name]!.pubkey) : hex(name));
const seed = (name: string): Uint8Array => hex(table.keys[name]!.seed);
/** An envelope by name, or an inline hex envelope. */
const envelope = (name: string): Uint8Array => hex(table.envelopes[name] ?? name);
const digestOf = (name: string): Uint8Array => grant.digest(envelope(name));

function validity(v: JsonGrant['validity']): grant.Validity {
  if (v === 'unbounded') return { finite: false };
  if (typeof v === 'object') return { finite: true, expiresAt: BigInt(v.expires_at) };
  // inherit is an issuance input, not an encoding: as a body it is nothing.
  return { finite: 'inherit' } as unknown as grant.Validity;
}

function toGrant(j: JsonGrant): grant.Grant {
  return {
    domain: j.domain,
    subject: key(j.subject),
    parent: j.parent === null ? undefined : table.envelopes[j.parent] ? digestOf(j.parent) : hex(j.parent),
    scope: j.scope ?? [],
    actions: j.actions ?? [],
    delegable: j.delegable ?? [],
    depth: j.depth,
    validity: validity(j.validity),
  };
}

function fromGrant(g: grant.Grant): unknown {
  return {
    domain: g.domain,
    subject: toHex(g.subject),
    parent: g.parent === undefined ? null : toHex(g.parent),
    scope: g.scope,
    actions: g.actions,
    delegable: g.delegable,
    depth: g.depth,
    validity: g.validity.finite ? { expires_at: Number(g.validity.expiresAt) } : 'unbounded',
  };
}

/** The table's spelling of a grant, with keys resolved, for a deep comparison. */
function expectedGrant(j: JsonGrant): unknown {
  return fromGrant(toGrant(j));
}

/** JSON with bigints spelled out, for a message. */
const show = (value: unknown): string =>
  JSON.stringify(value, (_, v: unknown) => (typeof v === 'bigint' ? v.toString() : v));
const time = (now: number | null | undefined): grant.Time =>
  now === null || now === undefined ? { present: false } : { present: true, now: BigInt(now) };
const root = (r: { key: string; domain: string }): grant.Root => ({ key: key(r.key), domain: r.domain });

function verifiedJson(v: grant.Verified): unknown {
  return {
    subject: toHex(v.subject),
    depth: v.depth,
    validity: v.validity.finite ? { expires_at: Number(v.validity.expiresAt) } : 'unbounded',
    hops: v.hops,
  };
}

const families: Record<string, (c: Case) => void> = {
  grant_encode(c) {
    if (c.refused) {
      assert.throws(
        () => grant.encode(toGrant(c.grant!)),
        (e: unknown) => e instanceof grant.GrantError && e.code === 'malformed',
      );
    } else {
      assert.equal(toHex(grant.encode(toGrant(c.grant!))), c.body);
    }
  },
  grant_decode(c) {
    const decoded = grant.decode(hex(c.body!));
    if (c.refused) assert.equal(decoded, c.refused);
    else assert.deepEqual(fromGrant(decoded as grant.Grant), expectedGrant(c.grant!));
  },
  grant_seal(c) {
    const sealed = grant.seal(seed(c.issuer!), toGrant(c.grant!));
    assert.equal(toHex(sealed), c.envelope);
    assert.equal(toHex(grant.digest(sealed)), c.digest);
  },
  grant_open(c) {
    const opened = grant.open(envelope(c.envelope!));
    if (c.refused) {
      assert.equal(opened, c.refused);
    } else {
      assert(typeof opened !== 'string');
      assert.equal(toHex(opened.issuer), key(c.issuer!) && toHex(key(c.issuer!)));
      assert.deepEqual(fromGrant(opened.grant), expectedGrant(c.grant!));
    }
  },
  grant_verify(c) {
    const chain = c.chain!.map(envelope);
    const outcome = grant.verify(root(c.root!), chain, c.request!, time(c.now));
    if (c.refused) {
      assert.deepEqual(outcome, c.refused);
      // Every refusal of steps 1, 2 and 4 holds inspect as it holds verify.
      const held = grant.inspect(root(c.root!), chain, time(c.now));
      if ((c.refused as grant.Refusal).code !== 'not_covered' && !(c.request!.domain !== c.root!.domain)) {
        assert.deepEqual(held, c.refused);
      }
    } else {
      assert(!grant.isRefusal(outcome));
      assert.deepEqual(verifiedJson(outcome), c.verified);
      const held = grant.inspect(root(c.root!), chain, time(c.now));
      assert.deepEqual(held, outcome);
    }
  },
  grant_issue(c) {
    const parents = (c.parents ?? []).map(envelope);
    const inherit = c.child!.validity === 'inherit';
    const child = toGrant(c.child!);
    if (inherit) child.validity = { finite: false };
    const outcome = grant.issue(root(c.root!), parents, seed(c.issuer!), child, inherit, time(c.now));
    if (c.refused) {
      assert.deepEqual(outcome, c.refused);
    } else {
      assert(outcome instanceof Uint8Array, `refused: ${show(outcome)}`);
      assert.equal(toHex(outcome), c.envelope);
      if (c.same_as) assert.equal(toHex(outcome), table.envelopes[c.same_as]);
      // What was sealed opens to the issuer and holds under the same root.
      const opened = grant.open(outcome);
      assert(typeof opened !== 'string');
      assert.equal(toHex(opened.issuer), toHex(getPublicKey(seed(c.issuer!))));
      const held = grant.inspect(root(c.root!), [...parents, outcome], time(c.now));
      assert(!grant.isRefusal(held), show(held));
    }
  },
};

const seen = new Map<string, number>();
for (const c of table.cases) {
  test(`${c.id} ${c.family}: ${c.name}`, () => {
    const run = families[c.family];
    assert(run, `unknown family ${c.family}`);
    run(c);
    seen.set(c.family, (seen.get(c.family) ?? 0) + 1);
  });
}

test('every case of the table was reproduced', () => {
  assert.equal(
    [...seen.values()].reduce((a, b) => a + b, 0),
    table.cases.length,
  );
  assert.equal(table.cases.length, 72);
});

test('decode never throws, whatever the bytes', () => {
  // A deterministic walk over the shapes that matter: every prefix of a
  // canonical body, every single-byte mutation of it, and noise.
  const body = hex(table.cases.find((c) => c.id === 'AUTH-GRANT-012')!.body!);
  for (let n = 0; n <= body.length; n++) grant.decode(body.slice(0, n));
  for (let i = 0; i < body.length; i++) {
    for (const delta of [1, 0x7f, 0xff]) {
      const mutated = body.slice();
      mutated[i] = (mutated[i]! + delta) & 0xff;
      const out = grant.decode(mutated);
      assert(typeof out === 'string' || typeof out === 'object');
    }
  }
  let x = 0x12345678;
  const next = () => (x = (x * 1103515245 + 12345) & 0x7fffffff);
  for (let round = 0; round < 500; round++) {
    const noise = new Uint8Array(next() % 300);
    for (let i = 0; i < noise.length; i++) noise[i] = next() & 0xff;
    grant.decode(noise);
    assert.equal(grant.open(noise), 'envelope_invalid');
  }
});

test('the work of verify is bounded by the chain bound, not by the input', () => {
  const r: grant.Root = { key: key('root'), domain: 'example.test' };
  const long = Array.from({ length: 1000 }, () => envelope('root_alice'));
  assert.deepEqual(
    grant.verify(r, long, { domain: 'example.test', action: 'read', scope: 'projects' }, { present: false }),
    { code: 'chain_too_long', hop: 0 },
  );
  const request = { domain: 'example.test', action: 'read', scope: 'projects/' + 'x/'.repeat(600) };
  assert.deepEqual(grant.verify(r, [envelope('root_alice')], request, { present: false }), {
    subject: key('alice'),
    depth: 3,
    validity: { finite: false },
    hops: 1,
  });
});
