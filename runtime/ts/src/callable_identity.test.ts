import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { callableIdentity } from './callable_identity.ts';
import { canonicalDeclaration, hashDeclaration, typeDeclaration, withDeclaration } from './declaration_identity.ts';
import { createValidator } from './validate.ts';
import type { Slots, TypeBinding, TypeExpression, Validator, WireFamily } from './validate.ts';

interface Source {
  name: string;
  family: string;
  wireExpression: TypeExpression;
  families: Record<string, { wire: string; declaration: string; digest: string; imports: string[] }>;
}
interface Slot {
  source?: string;
  type?: TypeExpression;
  slots?: Record<string, Slot>;
}
interface Case {
  id: string;
  source: string;
  slots?: Record<string, Slot>;
  path?: string;
  digest?: string;
  error?: string;
}
const sources = new Map<string, Source>(
  (
    JSON.parse(
      readFileSync(new URL('../../../conformance/tables/declaration-digests.json', import.meta.url), 'utf8'),
    ) as { cases: Source[] }
  ).cases.map((row) => [row.name, row]),
);
const table = JSON.parse(
  readFileSync(new URL('../../../conformance/tables/callable-identities.json', import.meta.url), 'utf8'),
) as { cases: Case[]; families: Record<string, { wire: WireFamily; declaration: unknown; imports?: string[] }> };

function fixture(source: string, type?: TypeExpression, slots: Record<string, Slot> = {}): TypeBinding {
  const row = sources.get(source);
  assert.ok(row, source);
  const validators: Record<string, Validator> = {},
    imports: Record<string, Record<string, Validator>> = {};
  for (const [name, family] of Object.entries(row.families)) {
    imports[name] = {};
    validators[name] = withDeclaration(
      createValidator(JSON.parse(family.wire) as WireFamily, family.digest, imports[name]),
      family.declaration,
    );
  }
  for (const [name, family] of Object.entries(row.families))
    for (const imported of family.imports) imports[name]![imported] = validators[imported]!;
  const bindings: Record<string, TypeBinding> = {};
  for (const [name, slot] of Object.entries(slots))
    bindings[name] = fixture(slot.source ?? source, slot.type, slot.slots);
  return { validate: validators[row.family]!, type: type ?? row.wireExpression, slots: bindings };
}

for (const row of table.cases)
  test(row.id, () => {
    const binding = fixture(row.source, undefined, row.slots);
    if (row.error) {
      assert.throws(
        () => callableIdentity(binding),
        (error) => error instanceof Error && error.message.includes(row.error!),
      );
      return;
    }
    const identity = callableIdentity(binding);
    assert.equal(identity.path, row.path);
    if (row.digest) assert.equal(identity.digest, row.digest);
    assert.equal(identity.digest, hashDeclaration(typeDeclaration(binding)));
    const reference: Record<string, unknown> = { binding: 'opaque', contract: identity.path, digest: identity.digest };
    binding.validate(binding.type, reference, '$', binding.slots);
    delete reference.digest;
    binding.validate(binding.type, reference, '$', binding.slots);
    reference.contract = 'same/Function';
    assert.throws(() => binding.validate(binding.type, reference, '$', binding.slots));
  });

function customFixtures(): Record<string, Validator> {
  const validators: Record<string, Validator> = {},
    imports: Record<string, Record<string, Validator>> = {};
  for (const [name, family] of Object.entries(table.families)) {
    imports[name] = {};
    const document = canonicalDeclaration(family.declaration);
    validators[name] = withDeclaration(
      createValidator(family.wire, hashDeclaration(document), imports[name]),
      document,
    );
  }
  for (const [name, family] of Object.entries(table.families))
    for (const imported of family.imports ?? []) imports[name]![imported] = validators[imported]!;
  return validators;
}

test('GEN-ID-CAPTURED-SCOPE preserves imported application and alias provenance', () => {
  const validators = customFixtures(),
    captured = validators.captured!,
    consumer = validators.consumer!;
  const slots: Slots = { T: { validate: captured, type: 'string' } };
  const binding: TypeBinding = { validate: captured, type: 'Callback', slots };
  const identity = callableIdentity(binding);
  assert.equal(identity.path, 'captured/Callback<string>');
  for (const type of ['Closed', { apply: 'captured.Callback', with: { T: 'string' } }] as TypeExpression[]) {
    assert.deepEqual(callableIdentity({ validate: consumer, type }), identity);
    consumer(type, { binding: 'opaque', contract: identity.path, digest: identity.digest });
  }
  assert.throws(() => callableIdentity({ validate: captured, type: 'Callback' }), /binding T/);
  assert.throws(() => captured('Callback', { binding: 'opaque', contract: 'captured/Callback' }), /binding T/);
  const plain = callableIdentity({ validate: consumer, type: 'Alias' });
  assert.equal(plain.path, 'captured/Plain');
  assert.equal(plain.digest, hashDeclaration(canonicalDeclaration(table.families.captured!.declaration)));
  assert.notEqual(plain.digest, hashDeclaration(canonicalDeclaration(table.families.consumer!.declaration)));
  const other = callableIdentity({
    validate: captured,
    type: 'Callback',
    slots: { T: { validate: captured, type: 'integer' } },
  });
  assert.notEqual(other.path, identity.path);
  assert.notEqual(other.digest, identity.digest);
  assert.throws(() => captured('Callback', { binding: 'opaque', contract: other.path }, '$', slots));
  const entity = callableIdentity({
    validate: captured,
    type: 'Callback',
    slots: { T: { validate: captured, type: { ref: 'Entity' } } },
  });
  assert.equal(entity.path, 'captured/Callback<&captured/Entity>');
  const drawn: TypeBinding = {
    validate: validators.drawn!,
    type: 'Callback',
    slots: { S: { name: 'captured', validate: captured, slots } },
  };
  const drawIdentity = callableIdentity(drawn);
  assert.equal(drawIdentity.path, 'drawn/Callback<captured<string>>');
  drawn.validate(
    drawn.type,
    { binding: 'opaque', contract: drawIdentity.path, digest: drawIdentity.digest },
    '$',
    drawn.slots,
  );
});

test('GEN-ID-REVISION-REFUSAL preserves the optional digest boundary', () => {
  const first = fixture('application argument declaration baseline'),
    second = fixture('application reachable argument changed');
  const identity = callableIdentity(first);
  assert.throws(
    () => second.validate(second.type, { binding: 'opaque', contract: identity.path, digest: identity.digest }),
    { code: 'contract_mismatch' },
  );
  second.validate(second.type, { binding: 'opaque', contract: identity.path });
  assert.throws(
    () =>
      callableIdentity({
        validate: createValidator({ types: { Plain: { kind: 'callable', contract: 'x/Plain' } } }, ''),
        type: 'Plain',
      }),
    /provenance/,
  );
});
