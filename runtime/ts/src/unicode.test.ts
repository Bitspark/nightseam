import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { createValidator, type WireFamily } from './validate.ts';
import { decodeEnvelope } from './envelope.ts';

test('Unicode scalar input domain is shared before decoding', () => {
  const table = JSON.parse(
    readFileSync(new URL('../../../conformance/tables/unicode.json', import.meta.url), 'utf8'),
  ) as { rows: { name: string; raw: string; valid: boolean }[] };
  const validate = createValidator({ types: {} });
  for (const row of table.rows) {
    const checkFrame = (): void => {
      decodeEnvelope('{"version":1,"kind":"event","event":"probe","data":' + row.raw + '}', 's:', 'c:');
    };
    if (row.valid) assert.doesNotThrow(checkFrame, row.name);
    else assert.throws(checkFrame, /invalid Unicode: expected Unicode scalar strings/, row.name);
    // A duplicate overwritten value is already lost after JSON.parse; it is
    // held at the raw frame boundary above rather than reconstructed here.
    if (row.name === 'overwritten malformed value') continue;
    const value: unknown = JSON.parse(row.raw);
    const checkValue = (): void => validate('json', value);
    const checkDescriptor = (): void => {
      createValidator({ types: {}, extension: value } as WireFamily);
    };
    for (const check of [checkValue, checkDescriptor]) {
      if (row.valid) assert.doesNotThrow(check, row.name);
      else assert.throws(check, /invalid Unicode: expected Unicode scalar strings/, row.name);
    }
  }
});

test('literal, enum, tag and supplied expressions cannot retain unpaired units', () => {
  for (const type of [
    { kind: 'alias', type: { literal: '\uD800' } },
    { kind: 'enum', values: ['\uDC00'] },
    { kind: 'union', tag: 'kind', value: 'value', variants: { ['\uD800']: { empty: true } } },
  ])
    assert.throws(() => createValidator({ types: { Probe: type } } as WireFamily), /invalid Unicode/);
  assert.throws(() => createValidator({ types: {} })({ literal: '\uD800' }, '�'), /invalid Unicode/);
});
