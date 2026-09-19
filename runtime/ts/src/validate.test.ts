import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { createValidator, type Validator, type WireType } from './validate.ts';

// The validator agrees with the conformance table every runtime is held
// to, case by case, on the wire description of a family and of one it
// refers to — on the verdict, and on the diagnostic where the row states
// one, which is the string the Go validator prints for the same value.
test('validator conformance', () => {
  const table = JSON.parse(readFileSync(new URL('../../../conformance/tables/validator.json', import.meta.url), 'utf8')) as {
    wire: Record<string, WireType>;
    imported: Record<string, Record<string, WireType>>;
    cases: { expression: unknown; value: unknown; valid: boolean; message?: string }[];
  };
  const imported: Record<string, Validator> = {};
  for (const [family, wire] of Object.entries(table.imported)) imported[family] = createValidator(wire);
  const validate = createValidator(table.wire, imported);
  for (const c of table.cases) {
    const where = `${JSON.stringify(c.value)} against ${JSON.stringify(c.expression)}`;
    let valid = true, message = '';
    try { validate(c.expression as never, c.value); } catch (error) { valid = false; message = (error as Error).message; }
    assert.equal(valid, c.valid, where);
    if (c.message !== undefined) assert.equal(message, c.message, where);
  }
  // A parameter's slot is the binding's to validate, and a validation without one is refused.
  assert.throws(() => validate('S.Envelope', {}), /binding of the parameter S/);
  const bound = { name: 'peer', validate: (type: string, value: unknown) => { if (type !== 'Envelope' || typeof value !== 'object') throw new Error('not an envelope'); } };
  validate('S.Envelope', {}, '$', { S: bound });
  assert.throws(() => validate('S.Envelope', 1, '$', { S: bound }));
});
