import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { decodeEnvelope } from './envelope.ts';
import { createValidator, type Validator, type WireFamily, type TypeExpression, type Slots } from './validate.ts';

test('every concrete document example validates with its visible bindings', () => {
  const table = JSON.parse(
    readFileSync(new URL('../../../conformance/tables/examples.json', import.meta.url), 'utf8'),
  ) as {
    schemas: Record<string, Record<string, WireFamily>>;
    rows: {
      corpus: string;
      family: string;
      path: string;
      expression?: TypeExpression;
      value?: unknown;
      bindings?: Record<string, { Type?: string; Family?: string }>;
      unavailable?: { Kind: string; Reason: string };
      to?: string;
      member?: string;
    }[];
  };
  let proof = 0;
  const checkouts: Record<string, Record<string, Validator>> = {};
  for (const [corpus, schemas] of Object.entries(table.schemas)) {
    const imported: Record<string, Validator> = {};
    for (const [name, wire] of Object.entries(schemas)) imported[name] = createValidator(wire, imported);
    checkouts[corpus] = imported;
  }
  for (const row of table.rows) {
    const imported = checkouts[row.corpus]!;
    const where = `${row.corpus}/${row.family}/${row.path}`;
    if (row.unavailable) {
      assert(!Object.hasOwn(row, 'value'), where);
      assert(row.unavailable.Reason, where);
      assert(['limit', 'impossible'].includes(row.unavailable.Kind), where);
      continue;
    }
    assert(Object.hasOwn(row, 'value'), where);
    if (row.family === 'proof') proof++;
    const validate = imported[row.family]!;
    const slots: Slots = Object.fromEntries(
      Object.entries(row.bindings ?? {}).map(([name, binding]) => {
        if (binding.Family) {
          assert(imported[binding.Family], where);
          return [name, { name: binding.Family, validate: imported[binding.Family]! }];
        }
        return [name, { type: binding.Type as TypeExpression, validate }];
      }),
    );
    assert.doesNotThrow(() => {
      let value = row.value;
      if (row.to) {
        const [local, remote] = row.to === 'client' ? ['c:', 's:'] : ['s:', 'c:'];
        decodeEnvelope(JSON.stringify(value), local, remote);
        if (row.member) value = (value as Record<string, unknown>)[row.member];
      }
      if (row.expression !== undefined) validate(row.expression, value, '$', slots);
    }, where);
  }
  assert(proof >= 30, `only ${proof} proof examples are held`);
});
