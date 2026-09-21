import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { createValidator, type Validator, type WireFamily, type TypeExpression, type Slots } from './validate.ts';

test('nested supplied type bindings retain their own declaration scopes', () => {
  const scalar = createValidator({ types: { Count: { kind: 'alias', type: 'integer' } } });
  const box = createValidator({
    types: { Box: { kind: 'record', parameters: [{ name: 'T' }], fields: [{ name: 'value', type: 'T' }] } },
  });
  const cell = createValidator({
    parameters: [{ name: 'T' }],
    types: { Request: { kind: 'record', fields: [{ name: 'value', type: 'T' }] } },
  });
  const count = { type: 'Count', validate: scalar };
  const slots: Slots = {
    T: { type: 'Box', validate: box, slots: { T: { type: 'Box', validate: box, slots: { T: count } } } },
  };
  cell('Request', { value: { value: { value: 7 } } }, '$', slots);
  assert.throws(() => cell('Request', { value: { value: { value: 'wrong' } } }, '$', slots), /integer/);
  const cyclic: Record<string, { type: string; validate: Validator; slots?: Slots }> = {};
  cyclic.T = { type: 'Box', validate: box, slots: cyclic };
  assert.throws(() => cell('Request', {}, '$', cyclic), /cyclic type argument bindings/);
});

// The validator agrees with the conformance table every runtime is held
// to, case by case, on the wire description of a family and of one it
// refers to — on the verdict, and on the diagnostic where the row states
// one, which is the string the Go validator prints for the same value.
test('validator conformance', () => {
  const table = JSON.parse(
    readFileSync(new URL('../../../conformance/tables/validator.json', import.meta.url), 'utf8'),
  ) as {
    wire: WireFamily;
    imported: Record<string, WireFamily>;
    patterns: { pattern: string; valid: boolean }[];
    patternValues: { pattern: string; value: string; valid: boolean }[];
    equivalence: { generic: TypeExpression; bound: TypeExpression; values: unknown[] }[];
    cases: {
      expression: unknown;
      value: unknown;
      valid: boolean;
      message?: string;
      slots?: Record<string, { type: TypeExpression } | { family: string }>;
    }[];
  };
  for (const row of table.patterns) {
    const create = (): Validator =>
      createValidator({
        types: {
          Probe: {
            kind: 'alias',
            type: {
              array: {
                nullable: {
                  kind: 'record',
                  fields: [{ name: 'tag', type: 'string', pattern: row.pattern }],
                },
              },
            },
          },
        },
      });
    if (row.valid) assert.doesNotThrow(create, row.pattern);
    else assert.throws(create, /outside Nightseam dialect/, row.pattern);
  }
  for (const row of table.patternValues) {
    const validate = createValidator({
      types: {
        Probe: { kind: 'record', fields: [{ name: 'text', type: 'string', required: true, pattern: row.pattern }] },
      },
    });
    const check = (): void => validate('Probe', { text: row.value });
    if (row.valid) assert.doesNotThrow(check, JSON.stringify(row));
    else assert.throws(check, { message: '$.text: expected a match of ' + row.pattern }, JSON.stringify(row));
  }
  const imported: Record<string, Validator> = {};
  for (const [family, wire] of Object.entries(table.imported)) imported[family] = createValidator(wire, imported);
  const validate = createValidator(table.wire, imported);
  for (const c of table.cases) {
    const where = `${JSON.stringify(c.value)} against ${JSON.stringify(c.expression)}`;
    let valid = true,
      message = '';
    try {
      const slots: Slots = Object.fromEntries(
        Object.entries(c.slots ?? {}).map(([name, binding]) => [
          name,
          'family' in binding
            ? { name: binding.family, validate: imported[binding.family]! }
            : { type: binding.type, validate },
        ]),
      );
      validate(c.expression as never, c.value, '$', slots);
    } catch (error) {
      valid = false;
      message = (error as Error).message;
    }
    assert.equal(valid, c.valid, where);
    if (c.message !== undefined) assert.equal(message, c.message, where);
  }
  // A parameter's slot is the binding's to validate, and a validation without one is refused.
  for (const law of table.equivalence) {
    const valid = (type: TypeExpression, value: unknown): boolean => {
      try {
        validate(type, value);
        return true;
      } catch {
        return false;
      }
    };
    for (const value of law.values)
      assert.equal(valid(law.generic, value), valid(law.bound, value), JSON.stringify({ law, value }));
  }
  assert.throws(() => validate('S.Envelope', {}), /binding of the parameter S/);
  const bound = {
    name: 'peer',
    validate: imported.peer!,
  };
  validate('S.Envelope', { version: 1 }, '$', { S: bound });
  assert.throws(() => validate('S.Envelope', 1, '$', { S: bound }));
});
