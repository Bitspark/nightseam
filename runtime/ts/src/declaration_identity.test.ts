import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { test } from 'node:test';
import { createValidator } from './validate.ts';
import type { TypeBinding, Validator, WireFamily } from './validate.ts';
import {
  boundDeclaration,
  canonicalDeclaration,
  declarationDigest,
  hashDeclaration,
  typeDeclaration,
  withDeclaration,
} from './declaration_identity.ts';

interface Fixture {
  name: string;
  declaration: string;
  digest: string;
  sources?: Record<string, Record<string, string>>;
}
const rows = new Map(
  (
    JSON.parse(
      readFileSync(new URL('../../../conformance/tables/declaration-digests.json', import.meta.url), 'utf8'),
    ) as { cases: Fixture[] }
  ).cases.map((row) => [row.name, row]),
);
function row(name: string): Fixture {
  const row = rows.get(name);
  assert.ok(row, name);
  return row;
}
function fixture(name: string): Validator {
  const selected = row(name),
    graph = JSON.parse(selected.declaration),
    family: WireFamily = { types: {} };
  for (const file of ['model.json', 'protocol.json', 'live.json']) {
    const source = selected.sources?.same?.[file];
    if (source) {
      const part = JSON.parse(source) as WireFamily;
      Object.assign(family.types, part.types);
      if (part.parameters) family.parameters = part.parameters;
    }
  }
  if (!graph.definitions.same) {
    graph.definitions.same = { kind: 'family', parameters: [], types: {} };
    graph.root = { ref: 'same' };
  }
  const document = canonicalDeclaration(graph);
  return withDeclaration(createValidator(family, hashDeclaration(document)), document);
}

test('canonical byte encoding and synchronous SHA-256 match every shared declaration fixture', () => {
  for (const selected of rows.values()) {
    assert.equal(canonicalDeclaration(JSON.parse(selected.declaration)), selected.declaration, selected.name);
    assert.equal(hashDeclaration(selected.declaration), selected.digest, selected.name);
  }
  for (const input of ['', 'abc', 'a'.repeat(1000000), '\u{10000}\ue000<>&\u2028\u2029'])
    assert.equal(hashDeclaration(input), createHash('sha256').update(input).digest('hex'));
  assert.equal(
    canonicalDeclaration({ '\u{10000}': 'a', '\ue000': '<>&\u2028\u2029' }),
    '{"\ue000":"\\u003c\\u003e\\u0026\\u2028\\u2029","\u{10000}":"a"}',
  );
});

test('closed identity retains ordered arguments, aliases, nested applications and same-path revisions', () => {
  const family = fixture('generic family template'),
    callable = fixture('callable template');
  const first = fixture('same named argument first revision'),
    second = fixture('same named argument second revision');
  const builtin = createValidator({ types: {} }, '');
  const integer: TypeBinding = { validate: builtin, type: 'integer' },
    text: TypeBinding = { validate: builtin, type: 'string' };
  const job1: TypeBinding = { validate: first, type: 'Job' },
    job2: TypeBinding = { validate: second, type: 'Job' };
  assert.equal(boundDeclaration(family, { A: integer, B: text }), row('bound generic family').declaration);
  assert.equal(
    boundDeclaration(family, { A: text, B: integer }),
    row('bound generic family changed arguments').declaration,
  );
  assert.equal(
    typeDeclaration({ validate: callable, type: 'Function', slots: { A: job1, B: job2 } }),
    row('same path distinct revisions in separate arguments').declaration,
  );
  assert.equal(
    typeDeclaration({ validate: callable, type: 'Function', slots: { A: job2, B: job1 } }),
    row('same path distinct revisions argument order changed').declaration,
  );
  assert.equal(typeDeclaration({ validate: callable, type: 'Alias' }), row('application baseline').declaration);
  assert.equal(
    boundDeclaration(family, { A: job1, B: integer }),
    row('bound generic family stable argument closure').declaration,
  );
  const nested: TypeBinding = { validate: callable, type: 'Function', slots: { A: integer, B: text } };
  const document = boundDeclaration(family, { A: nested, B: job1 });
  assert.ok(document.includes(row('application baseline').declaration));
  assert.equal(declarationDigest(family, { A: nested, B: job1 }), hashDeclaration(document));
  assert.notEqual(declarationDigest(family, { A: nested, B: job1 }), declarationDigest(family, { A: nested, B: job2 }));
  assert.throws(() => declarationDigest(family), /missing required binding A/);
  assert.throws(() => declarationDigest(family, { A: integer }), /missing required binding B/);
  assert.throws(
    () => typeDeclaration({ validate: createValidator({ types: { Job: { kind: 'record' } } }, ''), type: 'Job' }),
    /provenance/,
  );
  assert.throws(
    () => withDeclaration(family, row('generic family template').declaration.replace('"version":1', '"version":2')),
    /version 1/,
  );
  assert.throws(
    () => withDeclaration(createValidator({ types: {} }, 'a'.repeat(64)), row('generic family template').declaration),
    /digest/,
  );
});

test('bound family arguments retain their own slots and refuse incomplete nested interpretations', () => {
  const family = fixture('generic family template'),
    graph = JSON.parse(row('generic family template').declaration);
  graph.definitions.same.parameters = [{ name: 'F', of: 'protocol' }];
  const document = canonicalDeclaration(graph);
  const consumer = withDeclaration(
    createValidator({ types: {}, parameters: [{ name: 'F', of: 'protocol' }] }, hashDeclaration(document)),
    document,
  );
  const builtin = createValidator({ types: {} }, '');
  const integer: TypeBinding = { validate: builtin, type: 'integer' },
    text: TypeBinding = { validate: builtin, type: 'string' };
  const got = boundDeclaration(consumer, { F: { name: 'same', validate: family, slots: { A: integer, B: text } } });
  assert.ok(got.includes(row('bound generic family').declaration));
  assert.throws(
    () => boundDeclaration(consumer, { F: { name: 'same', validate: family } }),
    /missing required binding A/,
  );
});
