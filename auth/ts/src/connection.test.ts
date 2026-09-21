/**
 * The authenticated connection held to conformance/tables/auth-boot.json:
 * the audience grammar, the binding and the proof, the exchange scripts, the
 * decision at the call, and the login transcripts with the record's state
 * after each step.
 */
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { fromHex, toHex } from './bytes.ts';
import * as auth from './connection.ts';
import * as grant from './grant.ts';

const table = JSON.parse(
  readFileSync(path.join(import.meta.dirname, '..', '..', '..', 'conformance', 'tables', 'auth-boot.json'), 'utf8'),
) as Table;

interface Table {
  root: { key: string; domain: string };
  keys: Record<string, { seed: string; pubkey: string }>;
  envelopes: Record<string, string>;
  cases: Case[];
}
interface Step {
  op: string;
  nonce?: string;
  subject?: string;
  proof?: string;
  chain?: string[];
  now?: number;
  id?: string;
  browser?: string;
  scope?: string[];
  valid_for?: number;
  principal?: string;
  authority?: string[];
  version?: number;
  result?: unknown;
  refused?: unknown;
  state?: string;
}
interface Case {
  family: string;
  id: string;
  name: string;
  url?: string;
  audience?: string;
  binding?: string;
  key?: string;
  nonce?: string;
  proof?: string;
  verifies?: boolean;
  refused?: unknown;
  steps?: Step[];
  subject?: string;
  chain?: string[];
  request?: grant.Request;
  now?: number;
  allowed?: unknown;
}

const hex = (text: string): Uint8Array => {
  const bytes = fromHex(text);
  assert(bytes !== undefined, `not hex: ${text}`);
  return bytes;
};
const key = (name: string): Uint8Array => hex(table.keys[name]!.pubkey);
const envelope = (name: string): Uint8Array => hex(table.envelopes[name]!);
/** The table's name for an envelope's bytes, or the hex when it names none. */
const envelopeName = (bytes: Uint8Array): string =>
  Object.keys(table.envelopes).find((k) => table.envelopes[k] === toHex(bytes)) ?? toHex(bytes);
const time = (now: number | undefined): grant.Time =>
  now === undefined ? { present: false } : { present: true, now: BigInt(now) };
const root: grant.Root = { key: key(table.root.key), domain: table.root.domain };
const validityJson = (v: grant.Validity): unknown => (v.finite ? { expires_at: Number(v.expiresAt) } : 'unbounded');

const families: Record<string, (c: Case) => void> = {
  boot_audience(c) {
    if (c.refused) assert.throws(() => auth.audience(c.url!), auth.AudienceError);
    else assert.equal(auth.audience(c.url!), c.audience);
  },
  auth_binding(c) {
    assert.equal(toHex(auth.binding(c.audience!)), c.binding);
  },
  auth_prove(c) {
    assert.equal(auth.verify(key(c.key!), c.audience!, hex(c.nonce!), hex(c.proof!)), c.verifies);
    if (c.verifies) assert.equal(toHex(auth.prove(hex(table.keys[c.key!]!.seed), c.audience!, hex(c.nonce!))), c.proof);
  },
  auth_connect(c) {
    const connection = new auth.Connection(root, c.audience === '' ? undefined : c.audience);
    for (const step of c.steps!) {
      if (step.op === 'challenge') {
        const outcome = connection.challenge(hex(step.nonce!));
        if (step.refused) assert.deepEqual(outcome, step.refused, step.op);
        else assert.deepEqual(outcome, { nonce: hex(step.nonce!) });
      } else if (step.op === 'prove') {
        const outcome = connection.prove(
          key(step.subject!),
          hex(step.proof!),
          step.chain!.map(envelope),
          time(step.now),
        );
        if (step.refused) {
          assert.deepEqual(outcome, step.refused, step.op);
        } else {
          assert(!auth.isRefusal(outcome));
          assert.deepEqual(
            { since: Number(outcome.since), subject: toHex(outcome.subject), validity: validityJson(outcome.validity) },
            step.result,
          );
          assert.deepEqual(connection.context(), outcome);
        }
      } else {
        assert.fail(`unknown op ${step.op}`);
      }
    }
  },
  auth_call(c) {
    const ctx: auth.Context | undefined =
      c.subject === undefined
        ? undefined
        : { subject: key(c.subject), chain: c.chain!.map(envelope), validity: { finite: false }, since: 0n };
    const outcome = auth.call(root, ctx, c.request!, time(c.now));
    if (c.refused) {
      assert.deepEqual(outcome, c.refused);
    } else {
      assert(!auth.isRefusal(outcome));
      assert.deepEqual(
        {
          depth: outcome.depth,
          hops: outcome.hops,
          subject: toHex(outcome.subject),
          validity: validityJson(outcome.validity),
        },
        c.allowed,
      );
    }
  },
  boot_login(c) {
    const store = new auth.MemoryStore();
    const service = new auth.Service(root, c.audience!, store);
    for (const step of c.steps!) {
      const now = BigInt(step.now!);
      const id = step.id === undefined ? undefined : hex(step.id);
      let outcome: unknown;
      switch (step.op) {
        case 'begin': {
          const r = service.begin(now, id!, hex(step.nonce!), key(step.browser!), step.scope!, step.valid_for ?? 0);
          outcome = 'code' in r ? r.code : { expires_at: Number(r.expiresAt), version: r.version };
          break;
        }
        case 'read': {
          const r = service.read(now, id!);
          outcome =
            'code' in r
              ? r.code
              : {
                  browser: toHex(r.browser),
                  expires_at: Number(r.expiresAt),
                  nonce: toHex(r.nonce),
                  scope: r.scope,
                  valid_for: r.validFor,
                };
          break;
        }
        case 'answer': {
          const r = service.answer(
            now,
            id!,
            key(step.principal!),
            hex(step.proof!),
            step.authority!.map(envelope),
            step.version!,
          );
          outcome = typeof r === 'string' ? r : r.grant ? { code: r.code, grant: r.grant } : r.code;
          break;
        }
        case 'collect': {
          const r = service.collect(now, id!, hex(step.proof!));
          outcome =
            'code' in r
              ? r.code
              : {
                  authority: r.authority.map(envelopeName),
                  possession: toHex(r.possession),
                  principal: toHex(r.principal),
                };
          break;
        }
        case 'sweep': {
          outcome = service.sweep(now).map(toHex);
          break;
        }
        default:
          assert.fail(`unknown op ${step.op}`);
      }
      if (step.refused !== undefined) assert.deepEqual(outcome, step.refused, `${step.op} at ${step.now}`);
      else assert.deepEqual(outcome, step.result, `${step.op} at ${step.now}`);
      if (step.state !== undefined)
        assert.equal(store.get(id!)?.state, step.state, `state after ${step.op} at ${step.now}`);
    }
  },
};

let reproduced = 0;
for (const c of table.cases) {
  test(`${c.id} ${c.family}: ${c.name}`, () => {
    const run = families[c.family];
    assert(run, `unknown family ${c.family}`);
    run(c);
    reproduced++;
  });
}

test('every case of the table was reproduced', () => {
  assert.equal(reproduced, table.cases.length);
  assert.equal(table.cases.length, 52);
});

test('the audience grammar never throws anything but its own refusal', () => {
  const inputs = [
    '',
    '://',
    'wss://',
    'wss://h',
    'http://h:',
    'http://h:99999/x',
    'http://[::1',
    'http://h/%zz',
    'http://h/a b',
    'http://h/a\u0000',
    'HTTP://H/A',
    'ws://h:80',
    'x'.repeat(5000),
  ];
  for (const url of inputs) {
    try {
      auth.audience(url);
    } catch (error) {
      assert(error instanceof auth.AudienceError, `${url}: ${String(error)}`);
    }
  }
  assert.equal(auth.audience('HTTP://H/A'), 'http://h/A');
  assert.equal(auth.audience('ws://h:80'), 'http://h');
});

test('terms render into the order they parse from', () => {
  const t = auth.parseTerms(['action:read', 'action:write', 'delegable:read', 'scope:projects/7', 'depth:2']);
  assert.deepEqual(t, { actions: ['read', 'write'], delegable: ['read'], scope: ['projects/7'], depth: 2 });
  assert.deepEqual(auth.renderTerms(t!, true), [
    'action:read',
    'action:write',
    'delegable:read',
    'scope:projects/7',
    'depth:2',
  ]);
  assert.equal(auth.parseTerms(['scope:a', 'action:b']), undefined);
  assert.equal(auth.parseTerms(['action:b', 'action:a']), undefined);
  assert.equal(auth.parseTerms(['delegable:x']), undefined);
  assert.equal(auth.parseTerms(['depth:0', 'depth:1']), undefined);
  assert.equal(auth.parseTerms(['depth:16']), undefined);
  assert.equal(auth.parseTerms(['depth:01']), undefined);
  assert.deepEqual(auth.parseTerms([]), { actions: [], delegable: [], scope: [], depth: 0 });
});
