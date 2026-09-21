/**
 * The exposure held to conformance/tables/auth-exposure.json: construction
 * and its refusals, rendering, and the scripts of bind, call, export, invoke
 * and emit with each decision — and, where an effect time is given, the
 * decision at the effect.
 */
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { fromHex, toHex } from './bytes.ts';
import type { Context } from './connection.ts';
import * as exposure from './exposure.ts';
import * as grant from './grant.ts';

const table = JSON.parse(
  readFileSync(path.join(import.meta.dirname, '..', '..', '..', 'conformance', 'tables', 'auth-exposure.json'), 'utf8'),
) as Table;

interface Table {
  root: { key: string; domain: string };
  keys: Record<string, { seed: string; pubkey: string }>;
  envelopes: Record<string, string>;
  contexts: Record<string, { subject: string; chain: string[] }>;
  surfaces: Record<string, { family: string; digest: string; members: { key: string; fields: string[] | null }[] }>;
  policies: Record<string, exposure.Policy>;
  cases: Case[];
}
interface Step {
  op: string;
  policy?: string;
  connection?: string;
  member?: string;
  route?: string;
  payload?: Record<string, unknown>;
  meta?: unknown;
  now?: number;
  effect_at?: number;
  state?: Record<string, string>;
  ref?: string;
  scope?: string;
  recipients?: string[];
  result?: unknown;
  refused?: unknown;
  effect?: unknown;
}
interface Case {
  family: string;
  id: string;
  name: string;
  policy?: string;
  routes?: string[];
  refused?: unknown;
  template?: string;
  payload?: Record<string, unknown>;
  export?: string;
  scope?: string;
  steps?: Step[];
}

const hex = (text: string): Uint8Array => {
  const bytes = fromHex(text);
  assert(bytes !== undefined, `not hex: ${text}`);
  return bytes;
};
const key = (name: string): Uint8Array => hex(table.keys[name]!.pubkey);
const envelope = (name: string): Uint8Array => hex(table.envelopes[name]!);
const time = (now: number | undefined): grant.Time =>
  now === undefined ? { present: false } : { present: true, now: BigInt(now) };
const root: grant.Root = { key: key(table.root.key), domain: table.root.domain };
/** JSON with bigints spelled out, for a message. */
const show = (value: unknown): string =>
  JSON.stringify(value, (_, v: unknown) => (typeof v === 'bigint' ? v.toString() : v));

function surface(name: string): exposure.Surface {
  const s = table.surfaces[name]!;
  return {
    family: s.family,
    digest: s.digest,
    members: s.members.map((m) => ({ key: m.key, fields: m.fields ?? [] })),
  };
}

function context(name: string | undefined): Context | undefined {
  if (name === undefined) return undefined;
  const c = table.contexts[name]!;
  return { subject: key(c.subject), chain: c.chain.map(envelope), validity: { finite: false }, since: 0n };
}

/** A decision as the table spells it: the members it names, subjects in hex, no verified grant. */
function decisionJson(d: exposure.Decision): unknown {
  const out: Record<string, unknown> = { kind: d.kind, member: d.member };
  if (d.subject !== undefined) out.subject = toHex(d.subject);
  if (d.action !== undefined) out.action = d.action;
  if (d.scope !== undefined) out.scope = d.scope;
  return out;
}

function condition(state: Record<string, string> | undefined): exposure.Condition | undefined {
  if (state === undefined) return undefined;
  return (scope) => (state[scope] === undefined ? { ok: true } : { ok: false, reason: state[scope]! });
}

const families: Record<string, (c: Case) => void> = {
  exposure_bind(c) {
    const policy = table.policies[c.policy!]!;
    const bound = exposure.bind(surface(policy.family), policy);
    if (c.refused) assert.deepEqual(bound, c.refused);
    else {
      assert(bound instanceof exposure.Binding, show(bound));
      assert.deepEqual(bound.routes(), c.routes);
    }
  },
  exposure_template(c) {
    const scope = exposure.render(c.template!, c.payload!, c.export ?? '');
    if (c.refused) assert.equal(scope, undefined);
    else assert.equal(scope, c.scope);
  },
  exposure(c) {
    let binding: exposure.Binding | undefined;
    for (const step of c.steps!) {
      const at = `${step.op} at ${step.now ?? '-'}`;
      switch (step.op) {
        case 'bind': {
          const policy = table.policies[step.policy!]!;
          const bound = exposure.bind(surface(policy.family), policy);
          assert(bound instanceof exposure.Binding, at);
          assert.equal(step.result, 'bound');
          binding = bound;
          break;
        }
        case 'call':
        case 'invoke': {
          assert(binding, at);
          const ctx = context(step.connection);
          const decided =
            step.op === 'call'
              ? binding.decide(root, step.member!, step.payload ?? {}, '', ctx, time(step.now))
              : binding.invoke(root, step.ref!, step.payload ?? {}, ctx, time(step.now));
          if (step.refused !== undefined) {
            assert.deepEqual(decided, step.refused, at);
            break;
          }
          assert(!exposure.isRefusal(decided), `${at}: ${show(decided)}`);
          assert.deepEqual(decisionJson(decided), step.result, at);
          if (step.effect_at !== undefined) {
            const refused = binding.effect(root, decided, ctx, time(step.effect_at), condition(step.state));
            assert.deepEqual(refused === undefined ? 'applied' : { refused }, step.effect, `effect of ${at}`);
          }
          break;
        }
        case 'export': {
          assert(binding, at);
          assert.equal(binding.export(step.ref!, step.member!, step.scope!) ? 'exported' : 'refused', step.result, at);
          break;
        }
        case 'emit': {
          assert(binding, at);
          const deliveries = binding.emit(
            root,
            step.member!,
            step.payload ?? {},
            step.recipients!.map((name) => ({ name, ctx: context(name) })),
            time(step.now),
          );
          const result: Record<string, unknown> = {};
          for (const d of deliveries)
            result[d.recipient] = d.refused === undefined ? 'delivered' : { dropped: d.refused };
          assert.deepEqual(result, step.result, at);
          break;
        }
        default:
          assert.fail(`unknown op ${step.op}`);
      }
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
  assert.equal(table.cases.length, 35);
});

test('a binding refuses construction whole and exposes nothing partial', () => {
  const s = surface('worker');
  const p = table.policies.P!;
  const less: exposure.Policy = {
    ...p,
    treatments: Object.fromEntries(
      Object.entries(p.treatments).filter(([k]) => k !== 'method:list' && k !== 'event:progress'),
    ),
  };
  assert.deepEqual(exposure.bind(s, less), { code: 'auth.member_unbound', members: ['event:progress', 'method:list'] });
  const bound = exposure.bind(s, p);
  assert(bound instanceof exposure.Binding);
  assert.deepEqual(bound.decide(root, 'method:stop', {}, '', context('alice'), time(1)), {
    code: 'auth.unknown_member',
  });
  assert.deepEqual(bound.invoke(root, 'nobody', {}, context('alice'), time(1)), { code: 'auth.reference_unknown' });
  assert.equal(bound.export('x', 'method:list', 'projects/7'), false);
  assert.equal(bound.export('x', 'callable:JobStatus', ''), false);
});
