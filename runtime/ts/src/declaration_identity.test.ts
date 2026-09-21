import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { test } from 'node:test';
import { createValidator } from './validate.ts';
import type { TypeBinding, TypeExpression, Validator, WireFamily } from './validate.ts';
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
  family?: string;
  wireExpression?: TypeExpression;
  families?: Record<string, { wire: string; declaration: string; digest: string; imports: string[] }>;
}
const rows = new Map(
  (
    JSON.parse(
      readFileSync(new URL('../../../conformance/tables/declaration-digests.json', import.meta.url), 'utf8'),
    ) as { cases: Fixture[] }
  ).cases.map((row) => [row.name, row]),
);

test('runtime interpretation reproduces every closed expression rendered into the shared table', () => {
  let checked = 0;
  for (const selected of rows.values()) {
    if (!selected.families) continue;
    const validators: Record<string, Validator> = {},
      imports: Record<string, Record<string, Validator>> = {};
    for (const [name, family] of Object.entries(selected.families)) {
      imports[name] = {};
      validators[name] = withDeclaration(
        createValidator(JSON.parse(family.wire), family.digest, imports[name]),
        family.declaration,
      );
    }
    for (const [name, family] of Object.entries(selected.families))
      for (const imported of family.imports) imports[name]![imported] = validators[imported]!;
    const derive = () =>
      selected.wireExpression
        ? typeDeclaration({ validate: validators[selected.family!]!, type: selected.wireExpression })
        : boundDeclaration(validators[selected.family!]!);
    const graph = JSON.parse(selected.declaration);
    if (!closedRoot(graph, graph.root)) {
      assert.throws(derive, Error, selected.name);
      continue;
    }
    let actual: string;
    try {
      actual = derive();
    } catch (error) {
      throw new Error(selected.name, { cause: error });
    }
    assert.equal(actual, selected.declaration, selected.name);
    checked++;
  }
  assert.ok(checked >= 30, 'shared table must exercise at least thirty runtime interpretations');
});

function closedRoot(graph: { definitions: Record<string, any> }, value: any): boolean {
  if (Array.isArray(value)) return value.every((child) => closedRoot(graph, child));
  if (!value || typeof value !== 'object') return true;
  if ('parameter' in value) return false;
  if (value.graph) return closedRoot(value.graph, value.graph.root);
  if (value.ref) {
    const definition = graph.definitions[value.ref];
    return !definition?.parameters?.length && !definition?.captures?.length;
  }
  return Object.values(value).every((child) => closedRoot(graph, child));
}
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

test('structural fields and containers keep independent graphs and pure aliases keep identity', () => {
  const first = fixture('same named argument first revision'),
    second = fixture('same named argument second revision');
  const shape: TypeExpression = {
    kind: 'record',
    fields: [
      { name: 'a', type: 'A', required: true },
      { name: 'b', type: { array: 'B' }, required: true, length: { min: 1 } },
    ],
  };
  const validate = createValidator(
    { types: { Alias: { kind: 'alias', parameters: [{ name: 'A' }, { name: 'B' }], type: shape } } },
    '',
  );
  const slots = { A: { validate: first, type: 'Job' }, B: { validate: second, type: 'Job' } };
  const direct = typeDeclaration({ validate, type: shape, slots });
  assert.equal(typeDeclaration({ validate, type: 'Alias', slots }), direct);
  assert.equal(typeDeclaration({ validate, type: { apply: 'Alias', with: { A: 'A', B: 'B' } }, slots }), direct);
  assert.ok(direct.includes(row('same named argument first revision').declaration));
  assert.ok(direct.includes(row('same named argument second revision').declaration));
  validate(shape, { a: { value: 'one' }, b: [{ value: 2 }] }, '$', slots);
  const primitive = typeDeclaration({ validate, type: 'integer' });
  assert.equal(
    typeDeclaration({ validate, type: { array: 'integer' } }),
    '{"definitions":{},"root":{"array":{"graph":' + primitive + '}},"version":1}',
  );
});

test('closed family identity and validation use the same nested type bindings', () => {
  const graph = JSON.parse(row('generic family template').declaration);
  graph.definitions.same.types = { Box: { ref: 'same/Box' } };
  graph.definitions['same/Box'] = {
    kind: 'record',
    parameters: [],
    captures: [{ name: 'A', of: '' }],
    open: false,
    fields: [{ name: 'value', type: { parameter: 'same/A' }, required: true, nullable: false, unique: false }],
  };
  const document = canonicalDeclaration(graph);
  const family = withDeclaration(
    createValidator(
      {
        types: { Box: { kind: 'record', fields: [{ name: 'value', type: 'A', required: true }] } },
        parameters: [{ name: 'A' }, { name: 'B' }],
      },
      hashDeclaration(document),
    ),
    document,
  );
  const consumer = createValidator(
    { types: { Draw: { kind: 'alias', type: 'F.Box' } }, parameters: [{ name: 'F', of: 'protocol' }] },
    '',
  );
  const builtin = createValidator({ types: {} }, '');
  const slots = { A: { validate: builtin, type: 'integer' }, B: { validate: builtin, type: 'string' } };
  assert.equal(declarationDigest(family, slots).length, 64);
  consumer('Draw', { value: 42 }, '$', { F: { name: 'same', validate: family, slots } });
  assert.throws(
    () => consumer('Draw', { value: 'wrong' }, '$', { F: { name: 'same', validate: family, slots } }),
    /integer/,
  );
  const nestedType = { validate: family, type: 'Box', slots };
  const echo = createValidator({ types: { Use: { kind: 'alias', type: 'T' } }, parameters: [{ name: 'T' }] }, '');
  echo('Use', { value: 42 }, '$', { T: nestedType });
  assert.throws(() => echo('Use', { value: 'wrong' }, '$', { T: nestedType }), /integer/);
});
