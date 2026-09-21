import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { createValidator, type Validator, type WireFamily, type TypeExpression, type Slots } from './validate.ts';
import { DuplexError } from './error.ts';
import { validateDrawnType } from './validate.ts';

test('draw obligations check the original supplied member and direct request shape', () => {
  const validate = createValidator(
    {
      parameters: [{ name: 'S', of: 'live' }],
      types: {
        Job: { kind: 'record', fields: [] },
        Choice: { kind: 'enum', values: ['one'] },
        Alias: { kind: 'alias', type: 'Job' },
        Call: { kind: 'callable', request: 'integer' },
        Generic: { kind: 'record', parameters: [{ name: 'T' }], fields: [] },
        Captured: { kind: 'record', fields: [{ name: 'job', type: 'S.Job' }] },
      },
    },
    '',
  );
  for (const [type, object, valid] of [
    ['Job', false, true],
    ['Job', true, true],
    ['Choice', false, true],
    ['Choice', true, false],
    ['Alias', false, false],
    ['Call', false, false],
    ['Generic', false, false],
    ['Captured', false, false],
    ['Missing', false, false],
  ] as const) {
    const check = () => validateDrawnType({ type, validate }, type, object);
    if (valid) assert.doesNotThrow(check, type);
    else assert.throws(check, /family binding/, type);
  }
  assert.throws(() => validateDrawnType({ type: 'Job', validate }, 'Alias', false), /plain member/);
});

test('declaration digests survive imported and bound validation', () => {
  const table = JSON.parse(
    readFileSync(new URL('../../../conformance/tables/digests.json', import.meta.url), 'utf8'),
  ) as {
    cases: { name: string; wire: string; digest: string }[];
  };
  for (const row of table.cases)
    assert.doesNotThrow(() => createValidator(JSON.parse(row.wire) as WireFamily, row.digest), row.name);
  const first = table.cases.find((row) => row.name === 'callable revision 1')!;
  const second = table.cases.find((row) => row.name === 'callable revision 2')!;
  assert(first && second && first.digest !== second.digest);
  for (const row of [first, second])
    createValidator(JSON.parse(row.wire) as WireFamily, row.digest)('Payload', { text: 'fits both' });
  const declared = createValidator(JSON.parse(first.wire) as WireFamily, first.digest);
  const consumer = createValidator(
    {
      types: {
        Box: { kind: 'record', parameters: [{ name: 'T' }], fields: [{ name: 'item', type: 'T', required: true }] },
      },
    },
    second.digest,
    { source: declared },
  );
  const base = createValidator(
    {
      types: {
        Holder: { kind: 'record', fields: [{ name: 'item', type: 'source.Report', required: true }] },
      },
    },
    second.digest,
    { source: declared },
  );
  const inherited = createValidator(
    {
      types: {
        Holder: { kind: 'record', extends: ['base.Holder'] },
      },
    },
    second.digest,
    { base },
  );
  const cases: { name: string; validate: Validator; expression: TypeExpression; slots?: Slots; wrap?: boolean }[] = [
    { name: 'direct', validate: declared, expression: 'Report' },
    { name: 'imported', validate: consumer, expression: 'source.Report' },
    {
      name: 'family binding',
      validate: consumer,
      expression: 'S.Report',
      slots: { S: { name: 'same', validate: declared } },
    },
    { name: 'type binding', validate: consumer, expression: 'T', slots: { T: { type: 'Report', validate: declared } } },
    {
      name: 'nested application',
      validate: consumer,
      expression: { apply: 'Box', with: { T: 'source.Report' } },
      wrap: true,
    },
    { name: 'inherited field', validate: inherited, expression: 'Holder', wrap: true },
  ];
  for (const row of cases) {
    for (const digest of [first.digest, second.digest, '']) {
      const reference = { binding: 'nonce.1', contract: 'same/Report', ...(digest === '' ? {} : { digest }) };
      const check = (): void =>
        row.validate(row.expression, row.wrap ? { item: reference } : reference, '$', row.slots);
      if (digest !== second.digest) assert.doesNotThrow(check, row.name);
      else
        assert.throws(
          check,
          (error: unknown) =>
            error instanceof DuplexError && error.code === 'contract_mismatch' && error.message.includes('same/Report'),
          row.name,
        );
    }
  }
  for (const expected of [first.digest, '']) {
    const validate = createValidator(JSON.parse(first.wire) as WireFamily, expected);
    assert.doesNotThrow(() => validate('Report', { binding: 'nonce.1', contract: 'same/Report' }));
    if (expected === '')
      assert.doesNotThrow(() =>
        validate('Report', { binding: 'nonce.1', contract: 'same/Report', digest: second.digest }),
      );
  }
});

test('schema refuses malformed declaration digests', () => {
  for (const digest of ['short', 'A'.repeat(64), 'g'.repeat(64), 'a'.repeat(63), 'a'.repeat(65)]) {
    assert.throws(() => createValidator({ types: {} }, digest), /expected empty or lowercase SHA-256 digest/);
  }
});

test('nested supplied type bindings retain their own declaration scopes', () => {
  const scalar = createValidator({ types: { Count: { kind: 'alias', type: 'integer' } } }, '');
  const box = createValidator(
    {
      types: { Box: { kind: 'record', parameters: [{ name: 'T' }], fields: [{ name: 'value', type: 'T' }] } },
    },
    '',
  );
  const cell = createValidator(
    {
      parameters: [{ name: 'T' }],
      types: { Request: { kind: 'record', fields: [{ name: 'value', type: 'T' }] } },
    },
    '',
  );
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
      createValidator(
        {
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
        },
        '',
      );
    if (row.valid) assert.doesNotThrow(create, row.pattern);
    else assert.throws(create, /outside Nightseam dialect/, row.pattern);
  }
  for (const row of table.patternValues) {
    const validate = createValidator(
      {
        types: {
          Probe: { kind: 'record', fields: [{ name: 'text', type: 'string', required: true, pattern: row.pattern }] },
        },
      },
      '',
    );
    const check = (): void => validate('Probe', { text: row.value });
    if (row.valid) assert.doesNotThrow(check, JSON.stringify(row));
    else assert.throws(check, { message: '$.text: expected a match of ' + row.pattern }, JSON.stringify(row));
  }
  const imported: Record<string, Validator> = {};
  for (const [family, wire] of Object.entries(table.imported)) imported[family] = createValidator(wire, '', imported);
  const validate = createValidator(table.wire, '', imported);
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
