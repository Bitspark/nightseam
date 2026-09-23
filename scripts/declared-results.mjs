import assert from 'node:assert/strict';

export function declaredInputs(fixture) {
  assert.equal(fixture.schemaVersion, 1);
  assert.ok(Array.isArray(fixture.nodes));
  assert.ok(Array.isArray(fixture.cases) && fixture.cases.length > 0);
  const ids = new Set();
  const cases = fixture.cases.map(({ expected, ...input }) => {
    assert.equal(typeof input.id, 'string');
    assert.ok(!ids.has(input.id), `duplicate expected case ${input.id}`);
    assert.notEqual(expected, undefined, `missing expectation ${input.id}`);
    ids.add(input.id);
    return input;
  });
  return structuredClone({ nodes: fixture.nodes, cases });
}

export function compareDeclared(fixture, rows) {
  declaredInputs(fixture);
  assert.ok(Array.isArray(rows));
  assert.equal(rows.length, fixture.cases.length, 'missing or extra observations');
  const observed = new Map();
  for (const row of rows) {
    assert.deepEqual(Object.keys(row).sort(), ['id', 'observations']);
    assert.ok(!observed.has(row.id), `duplicate observation ${row.id}`);
    observed.set(row.id, row.observations);
  }
  for (const test of fixture.cases) {
    assert.ok(observed.has(test.id), `missing observation ${test.id}`);
    assert.deepEqual(observed.get(test.id), test.expected, `declared composition case ${test.id}`);
  }
}
