/**
 * Whole-contract holes and simultaneous substitution. TypeScript is the metalanguage.
 * Both encodings consume represented artifacts; neither transformation nor plugging
 * reconstructs its output artifact from the subject field.
 */
import assert from 'node:assert/strict';
import { isDeepStrictEqual } from 'node:util';
import type {
  Contract,
  ContractKey,
  ContractNode,
  ContractPath,
  ContractSubstitution,
  Generic,
  GenericId,
  Representation,
  RepresentedSubstitution,
  RepresentationSubstitution,
  Selection,
  Substitution,
  SubstitutionApplication,
  SubstitutionTransparency,
  TransformationFamily,
} from '../model.ts';

type O = string;
export const node = <G extends GenericId = never>(
  value?: O,
  children: readonly (readonly [ContractKey, Contract<O, G>])[] = [],
): ContractNode<O, G> => ({ kind: 'node', ...(value === undefined ? {} : { value }), children: new Map(children) });

// The conditional Generic<never> is empty; supplying an id witnesses G is inhabited.
export const generic = <G extends GenericId>(id: G): Generic<G> => ({ kind: 'generic', id }) as Generic<G>;

export const substitute: ContractSubstitution<O> = (template, bindings) => {
  if (template.kind === 'generic') return bindings(template.id); // Do not substitute again inside the replacement.
  return {
    ...template,
    children: new Map([...template.children].map(([key, child]) => [key, substitute(child, bindings)])),
  };
};
export const identity = <G extends GenericId>(id: G): Contract<O, G> => generic(id);
export const compose =
  <G extends GenericId, H extends GenericId, I extends GenericId>(
    first: Substitution<O, G, H>,
    second: Substitution<O, H, I>,
  ): Substitution<O, G, I> =>
  (id) =>
    substitute(first(id), second);

export function contractAt<G extends GenericId>(
  contract: Contract<O, G>,
  path: ContractPath,
): Selection<Contract<O, G>> {
  let selected = contract;
  for (const key of path) {
    if (selected.kind === 'generic') return { kind: 'missing' }; // Interior not known until instantiation.
    const child = selected.children.get(key);
    if (!child) return { kind: 'missing' };
    selected = child;
  }
  return { kind: 'found', value: selected };
}
function requireAt<G extends GenericId>(contract: Contract<O, G>, path: ContractPath): Contract<O, G> {
  const result = contractAt(contract, path);
  assert.equal(result.kind, 'found');
  return result.value;
}
function paths<G extends GenericId>(contract: Contract<O, G>): ContractPath[] {
  return contract.kind === 'generic'
    ? [[]]
    : [[], ...[...contract.children].flatMap(([key, child]) => paths(child).map((path) => [key, ...path]))];
}

export const nestedAt = { '0:encoding': 'nested', '1:language': null } as const;
export const flatAt = { ...nestedAt, '0:encoding': 'flat' } as const;
type Nested =
  | { readonly kind: 'generic'; readonly id: GenericId }
  | { readonly kind: 'node'; readonly value?: O; readonly children: readonly (readonly [ContractKey, Nested])[] };
type Row = { readonly path: ContractPath } & (
  { readonly kind: 'generic'; readonly id: GenericId } | { readonly kind: 'node'; readonly value?: O }
);
interface NestedArtifact {
  readonly kind: 'nested';
  readonly root: Nested;
}
interface FlatArtifact {
  readonly kind: 'flat';
  readonly rows: readonly Row[];
}
const localValue = (source: { readonly value?: O }) => (source.value === undefined ? {} : { value: source.value });
const orderKeys = (a: string, b: string) => (a < b ? -1 : a > b ? 1 : 0);

function encode(template: Contract<O, GenericId>): Nested {
  return template.kind === 'generic'
    ? { kind: 'generic', id: template.id }
    : {
        kind: 'node',
        ...localValue(template),
        children: [...template.children]
          .sort(([a], [b]) => orderKeys(a, b))
          .map(([key, child]) => [key, encode(child)]),
      };
}
function flatten(tree: Nested, path: ContractPath = []): Row[] {
  return tree.kind === 'generic'
    ? [{ kind: 'generic', path, id: tree.id }]
    : [
        { kind: 'node', path, ...localValue(tree) },
        ...tree.children.flatMap(([key, child]) => flatten(child, [...path, key])),
      ];
}
function readNested(artifact: unknown): NestedArtifact {
  assert.ok(artifact && typeof artifact === 'object' && 'kind' in artifact && artifact.kind === 'nested');
  return artifact as NestedArtifact; // Lawful artifacts use the declared encoding.
}
function readFlat(artifact: unknown): FlatArtifact {
  assert.ok(artifact && typeof artifact === 'object' && 'kind' in artifact && artifact.kind === 'flat');
  return artifact as FlatArtifact;
}
export function represent<C extends Contract<O, GenericId>>(subject: C): Representation<C, typeof nestedAt> {
  return {
    subject,
    coordinates: nestedAt,
    artifact: { kind: 'nested', root: encode(subject) } satisfies NestedArtifact,
  };
}
export const toFlat: TransformationFamily<O, typeof nestedAt, typeof flatAt, GenericId> = (input) => ({
  subject: input.subject,
  coordinates: flatAt,
  artifact: { kind: 'flat', rows: flatten(readNested(input.artifact).root) } satisfies FlatArtifact,
});

function applyNested(tree: Nested, argument: (id: GenericId) => Nested): Nested {
  return tree.kind === 'generic'
    ? argument(tree.id)
    : {
        ...tree,
        children: tree.children.map(([key, child]) => [key, applyNested(child, argument)]),
      };
}
function applyFlat(rows: readonly Row[], argument: (id: GenericId) => readonly Row[]): Row[] {
  return rows.flatMap<Row>((row) =>
    row.kind === 'node'
      ? [row]
      : argument(row.id).map((part) => ({
          ...part,
          path: [...row.path, ...part.path],
        })),
  );
}

function verifyApplication<
  G extends GenericId,
  H extends GenericId,
  C extends Contract<O, G>,
  D extends Contract<O, H>,
>(template: C, bindings: Substitution<O, G, H>, application: SubstitutionApplication<O, G, H, C, D>): void {
  assert.equal(application.template, template);
  assert.equal(application.bindings, bindings);
  assert.deepEqual(
    application.result,
    substitute(template, bindings),
    'The result subject is this substitution application.',
  );
}

// Artifact encodings erase the static G. Resolve only an id present in the input
// subject; this validates the narrowing back to G before looking up an argument.
function resolve<G extends GenericId, H extends GenericId>(
  template: Contract<O, G>,
  bindings: Substitution<O, G, H>,
  id: GenericId,
): { readonly id: G; readonly subject: Contract<O, H> } {
  if (template.kind === 'generic') {
    assert.equal(template.id, id);
    return { id: template.id, subject: bindings(template.id) };
  }
  for (const child of template.children.values()) {
    if (freeIds(child).has(id)) return resolve(child, bindings, id);
  }
  throw new Error(`Unknown generic id ${id}`);
}
function freeIds<G extends GenericId>(template: Contract<O, G>): Set<GenericId> {
  return template.kind === 'generic'
    ? new Set([template.id])
    : new Set([...template.children.values()].flatMap((child) => [...freeIds(child)]));
}

export const substituteNested: RepresentationSubstitution<O, typeof nestedAt> = (input, bindings, application) => {
  verifyApplication(input.subject, bindings.semantic, application);
  assert.deepEqual(readNested(input.artifact).root, encode(input.subject));
  const root = applyNested(readNested(input.artifact).root, (id) => {
    const expected = resolve(input.subject, bindings.semantic, id);
    const argument = bindings.argument(expected.id);
    assert.deepEqual(argument.subject, expected.subject);
    assert.deepEqual(argument.coordinates, nestedAt);
    assert.deepEqual(readNested(argument.artifact).root, encode(expected.subject));
    return readNested(argument.artifact).root;
  });
  return {
    subject: application.result,
    coordinates: nestedAt,
    artifact: { kind: 'nested', root } satisfies NestedArtifact,
  };
};
export const substituteFlat: RepresentationSubstitution<O, typeof flatAt> = (input, bindings, application) => {
  verifyApplication(input.subject, bindings.semantic, application);
  assert.deepEqual(readFlat(input.artifact).rows, flatten(encode(input.subject)));
  const rows = applyFlat(readFlat(input.artifact).rows, (id) => {
    const expected = resolve(input.subject, bindings.semantic, id);
    const argument = bindings.argument(expected.id);
    assert.deepEqual(argument.subject, expected.subject);
    assert.deepEqual(argument.coordinates, flatAt);
    assert.deepEqual(readFlat(argument.artifact).rows, flatten(encode(expected.subject)));
    return readFlat(argument.artifact).rows;
  });
  return { subject: application.result, coordinates: flatAt, artifact: { kind: 'flat', rows } satisfies FlatArtifact };
};

export const substitutionLaw = {
  transform: toFlat,
  sourceSubstitute: substituteNested,
  targetSubstitute: substituteFlat,
  equivalent: (left, right) =>
    left.subject === right.subject &&
    isDeepStrictEqual(left.coordinates, right.coordinates) &&
    isDeepStrictEqual(left.artifact, right.artifact),
} satisfies SubstitutionTransparency<O, typeof nestedAt, typeof flatAt>;

function representedBindings<G extends GenericId, H extends GenericId>(
  semantic: Substitution<O, G, H>,
): RepresentedSubstitution<O, G, H, typeof nestedAt> {
  return { semantic, argument: (id) => represent(semantic(id)) };
}
function application<G extends GenericId, H extends GenericId>(
  template: Contract<O, G>,
  bindings: Substitution<O, G, H>,
) {
  return { template, bindings, result: substitute(template, bindings) };
}
export function checkSubstitutionTransparency<G extends GenericId, H extends GenericId>(
  template: Contract<O, G>,
  semantic: Substitution<O, G, H>,
): void {
  const input = represent(template),
    bindings = representedBindings(semantic),
    witness = application(template, semantic);
  const transformedBindings = { semantic, argument: (id: G) => toFlat(bindings.argument(id)) };
  const fillThenTransform = toFlat(substituteNested(input, bindings, witness));
  const transformThenFill = substituteFlat(toFlat(input), transformedBindings, witness);
  assert.ok(substitutionLaw.equivalent(fillThenTransform, transformThenFill));
  assert.deepEqual(readFlat(fillThenTransform.artifact).rows, flatten(encode(witness.result)));
}

// The SAME generic occurs twice; both occurrences receive the same whole contract.
export const pair = node<'g0'>('Pair', [
  ['first', generic('g0')],
  ['second', generic('g0')],
]);
export const list = node<'h0'>('List', [['element', generic('h0')]]);
export const bool = node('Bool');
export const sigma: Substitution<O, 'g0', 'h0'> = () => list;
export const tau: Substitution<O, 'h0', never> = () => bool;
export const pairOfLists = substitute(pair, sigma);
export const closedPair = substitute(pairOfLists, tau);

const empty = node();
const closedBindings: Substitution<O, never, never> = (id) => {
  throw new Error(`No closed-contract hole exists: ${id}`);
};
const twoHoles = node<'g0' | 'g1'>('Operation', [
  ['input', generic('g0')],
  ['output', generic('g1')],
]);
const closedTable = { g0: empty, g1: bool } as const;
const closeTwo: Substitution<O, 'g0' | 'g1', never> = (id) => closedTable[id];

for (const template of [pair, generic('g0'), node<'g0'>()]) {
  assert.deepEqual(substitute(template, identity<'g0'>), template);
  assert.deepEqual(substitute(substitute(template, sigma), tau), substitute(template, compose(sigma, tau)));
  checkSubstitutionTransparency(template, sigma);
  checkSubstitutionTransparency(template, compose(sigma, tau));
  for (const path of paths(template)) {
    assert.deepEqual(requireAt(substitute(template, sigma), path), substitute(requireAt(template, path), sigma));
  }
}
checkSubstitutionTransparency(pairOfLists, tau);
checkSubstitutionTransparency(twoHoles, closeTwo);
checkSubstitutionTransparency(empty, closedBindings);
assert.deepEqual(substitute(empty, closedBindings), empty);
assert.deepEqual(compose(identity<'g0'>, sigma)('g0'), sigma('g0'));
assert.deepEqual(compose(sigma, identity<'h0'>)('g0'), sigma('g0'));
assert.deepEqual(
  compose(compose(sigma, tau), closedBindings)('g0'),
  compose(sigma, compose(tau, closedBindings))('g0'),
);

// Navigation can enter the replacement after crossing a former hole occurrence.
assert.equal(contractAt(pair, ['first', 'element']).kind, 'missing');
assert.deepEqual(requireAt(pairOfLists, ['first', 'element']), generic('h0'));
assert.deepEqual(requireAt(closedPair, ['first', 'element']), bool);
assert.equal(contractAt(substitute(pair, sigma), ['absent']).kind, 'missing');
assert.deepEqual(requireAt(pairOfLists, ['first']), requireAt(pairOfLists, ['second']));

// Simultaneous means exactly one pass. Same-named holes in a replacement survive.
const selfShaped = node<'g0'>('Box', [['inside', generic('g0')]]);
const selfBinding: Substitution<O, 'g0', 'g0'> = () => selfShaped;
assert.equal(substitute(generic('g0'), selfBinding), selfShaped);
checkSubstitutionTransparency(pair, selfBinding);

// A filled-template subject and its represented arguments cannot be fabricated.
const witness = application(pair, sigma);
assert.throws(() =>
  substituteNested(represent(pair), representedBindings(sigma), { ...witness, result: node<'h0'>('Wrong') }),
);
assert.throws(() =>
  substituteNested(represent(pair), { semantic: sigma, argument: () => represent(node<'h0'>('Wrong')) }, witness),
);

// Representation substitution itself satisfies identity and staged composition.
// Choose the same abstract result subject when comparing representations.
const idWitness = { template: pair, bindings: identity<'g0'>, result: pair };
const nestedIdentity = substituteNested(represent(pair), representedBindings(identity<'g0'>), idWitness);
assert.equal(nestedIdentity.subject, pair);
assert.deepEqual(nestedIdentity.artifact, represent(pair).artifact);
const first = substituteNested(represent(pair), representedBindings(sigma), witness);
const composed = compose(sigma, tau),
  oneWitness = application(pair, composed);
const secondWitness = { template: witness.result, bindings: tau, result: oneWitness.result };
const staged = substituteNested(first, representedBindings(tau), secondWitness);
const once = substituteNested(represent(pair), representedBindings(composed), oneWitness);
assert.equal(staged.subject, once.subject);
assert.deepEqual(staged.artifact, once.artifact);

function flatBindings<G extends GenericId, H extends GenericId>(semantic: Substitution<O, G, H>) {
  return { semantic, argument: (id: G) => toFlat(represent(semantic(id))) };
}
const flatIdentity = substituteFlat(toFlat(represent(pair)), flatBindings(identity<'g0'>), idWitness);
assert.ok(substitutionLaw.equivalent(flatIdentity, toFlat(represent(pair))));
const firstFlat = substituteFlat(toFlat(represent(pair)), flatBindings(sigma), witness);
const stagedFlat = substituteFlat(firstFlat, flatBindings(tau), secondWitness);
const onceFlat = substituteFlat(toFlat(represent(pair)), flatBindings(composed), oneWitness);
assert.ok(substitutionLaw.equivalent(stagedFlat, onceFlat));

// Compose represented arguments themselves, rather than only their semantic bindings.
const composedArguments = {
  semantic: composed,
  argument: (id: 'g0') => substituteNested(represent(sigma(id)), representedBindings(tau), application(sigma(id), tau)),
};
const throughArguments = substituteNested(represent(pair), composedArguments, oneWitness);
assert.equal(throughArguments.subject, staged.subject);
assert.deepEqual(throughArguments.artifact, staged.artifact);

console.log({
  holes: 'entire sub-contracts',
  example: 'Pair[g0][g0 := List[h0]][h0 := Bool]',
  repeatedOccurrences: 'consistent',
  substitution: 'identity and composition checked',
  navigation: 'existing paths and paths entering inserted contracts checked',
  transformationTransparency: 'fill then flatten = flatten then fill',
});
