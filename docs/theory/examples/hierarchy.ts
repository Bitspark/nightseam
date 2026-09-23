/**
 * Hierarchical instances of the coordinate model; TypeScript is the metalanguage.
 * Opaque value/key IDs are preserved between nested and flat tree encodings.
 * Transformations consume artifacts; they do not reconstruct them from subject.
 * Run from the repository root: pnpm --filter @nightseam/theory test
 */
import assert from 'node:assert/strict';
import { isDeepStrictEqual } from 'node:util';
import type {
  Contract,
  ContractKey,
  ContractLocation,
  ContractNavigation,
  ContractPath,
  ContractRepresentationMap,
  Coordinates,
  FamilyPath,
  FamilyPathMeaning,
  NavigationTransparency,
  Representation,
  RepresentationEquivalence,
  RepresentationNavigation,
  RepresentationObservation,
  Selection,
  TransformationFamily,
} from '../model.ts';

type O = string; // The implementation treats these strings as opaque value IDs.
const node = (value?: O, entries: readonly (readonly [ContractKey, Contract<O>])[] = []): Contract<O> => ({
  kind: 'node',
  ...(value === undefined ? {} : { value }),
  children: new Map(entries),
});

export const booleanCellTree = node('BooleanCell', [
  [
    'operations',
    node(undefined, [
      [
        'read',
        node('operation', [
          ['arguments', node()],
          ['result', node('Bool')],
        ]),
      ],
      [
        'write',
        node('operation', [
          ['arguments', node(undefined, [['value', node('Bool')]])],
          ['result', node('Unit')],
        ]),
      ],
    ]),
  ],
]);
export const emptyContract = node();

export const contractAt: ContractNavigation<O> = (contract, path) => {
  let selected = contract;
  for (const key of path) {
    const child = selected.children.get(key);
    if (child === undefined) return { kind: 'missing' };
    selected = child;
  }
  return { kind: 'found', value: selected };
};

export function locate<C extends Contract<O>>(root: C, path: ContractPath): Selection<ContractLocation<O, C>> {
  const result = contractAt(root, path);
  return result.kind === 'missing' ? result : { kind: 'found', value: { root, path, selected: result.value } };
}

// Two independent axes; neither is a contract-child key. A higher slot is empty.
export const nestedAscending = { '0:layout': 'nested', '0:order': 'ascending', '1:execution': null } as const;
export const flatAscending = { ...nestedAscending, '0:layout': 'flat' } as const;
export const nestedDescending = { ...nestedAscending, '0:order': 'descending' } as const;
export const flatDescending = { ...flatAscending, '0:order': 'descending' } as const;

interface Nested {
  readonly value?: O;
  readonly children: readonly (readonly [ContractKey, Nested])[];
}
interface FlatNode {
  readonly path: ContractPath;
  readonly value?: O;
}
interface NestedArtifact {
  readonly kind: 'nested';
  readonly root: Nested;
}
interface FlatArtifact {
  readonly kind: 'flat';
  readonly nodes: readonly FlatNode[];
}
type Order = 'ascending' | 'descending';
const valueOf = (source: { readonly value?: O }): { readonly value?: O } =>
  source.value === undefined ? {} : { value: source.value };
const compareIds = (a: string, b: string): number => (a < b ? -1 : a > b ? 1 : 0);

function render(contract: Contract<O>): Nested {
  return {
    ...valueOf(contract),
    children: [...contract.children].sort(([a], [b]) => compareIds(a, b)).map(([key, child]) => [key, render(child)]),
  };
}

function nestedArtifact(artifact: unknown): NestedArtifact {
  assert.ok(artifact !== null && typeof artifact === 'object' && 'kind' in artifact && artifact.kind === 'nested');
  // A coordinate-specific decoding boundary; lawful inputs have this encoding.
  return artifact as NestedArtifact;
}
function flatArtifact(artifact: unknown): FlatArtifact {
  assert.ok(artifact !== null && typeof artifact === 'object' && 'kind' in artifact && artifact.kind === 'flat');
  return artifact as FlatArtifact;
}

function flatten(root: Nested, path: ContractPath = []): FlatNode[] {
  // Include EVERY node, even one with no value and no children.
  return [{ path, ...valueOf(root) }, ...root.children.flatMap(([key, child]) => flatten(child, [...path, key]))];
}
function reverseChildren(root: Nested): Nested {
  return {
    ...valueOf(root),
    children: [...root.children].reverse().map(([key, child]) => [key, reverseChildren(child)]),
  };
}
function comparePaths(a: ContractPath, b: ContractPath, order: Order): number {
  for (let index = 0; index < Math.min(a.length, b.length); index++) {
    const compared = compareIds(a[index], b[index]);
    if (compared !== 0) return order === 'ascending' ? compared : -compared;
  }
  return a.length - b.length; // A parent always precedes its children.
}

export function represent<C extends Contract<O>>(subject: C): Representation<C, typeof nestedAscending> {
  return {
    subject,
    coordinates: nestedAscending,
    artifact: { kind: 'nested', root: render(subject) } satisfies NestedArtifact,
  };
}

export const flattenAscending: TransformationFamily<O, typeof nestedAscending, typeof flatAscending> = (input) => ({
  subject: input.subject,
  coordinates: flatAscending,
  artifact: { kind: 'flat', nodes: flatten(nestedArtifact(input.artifact).root) } satisfies FlatArtifact,
});
export const reverseNested: TransformationFamily<O, typeof nestedAscending, typeof nestedDescending> = (input) => ({
  subject: input.subject,
  coordinates: nestedDescending,
  artifact: { kind: 'nested', root: reverseChildren(nestedArtifact(input.artifact).root) } satisfies NestedArtifact,
});
export const reverseFlat: TransformationFamily<O, typeof flatAscending, typeof flatDescending> = (input) => ({
  subject: input.subject,
  coordinates: flatDescending,
  artifact: {
    kind: 'flat',
    nodes: [...flatArtifact(input.artifact).nodes].sort((a, b) => comparePaths(a.path, b.path, 'descending')),
  } satisfies FlatArtifact,
});
export const flattenDescending: TransformationFamily<O, typeof nestedDescending, typeof flatDescending> = (input) => ({
  subject: input.subject,
  coordinates: flatDescending,
  artifact: { kind: 'flat', nodes: flatten(nestedArtifact(input.artifact).root) } satisfies FlatArtifact,
});

function verifyLocation<Root extends Contract<O>, Selected extends Contract<O>>(
  subject: Root,
  location: ContractLocation<O, Root, never, Selected>,
): void {
  assert.equal(location.root, subject, 'A location belongs to this root contract.');
  const result = contractAt(subject, location.path);
  assert.equal(result.kind, 'found', 'A location must name an existing child.');
  if (result.kind === 'found')
    assert.equal(result.value, location.selected, 'The selected subject must match the path.');
}

export function nestedNavigation<K extends Coordinates>(): RepresentationNavigation<O, K> {
  return (input, location) => {
    verifyLocation(input.subject, location);
    let selected = nestedArtifact(input.artifact).root;
    for (const key of location.path) {
      const child = selected.children.find(([candidate]) => candidate === key);
      assert.ok(child, 'A lawful representation exposes every contract child.');
      selected = child[1];
    }
    return {
      subject: location.selected,
      coordinates: input.coordinates,
      artifact: { kind: 'nested', root: selected } satisfies NestedArtifact,
    };
  };
}
export function flatNavigation<K extends Coordinates>(): RepresentationNavigation<O, K> {
  return (input, location) => {
    verifyLocation(input.subject, location);
    const nodes = flatArtifact(input.artifact)
      .nodes.filter((node) => location.path.every((key, index) => node.path[index] === key))
      .map((node) => ({ ...valueOf(node), path: node.path.slice(location.path.length) }));
    assert.ok(
      nodes.some((node) => node.path.length === 0),
      'Even an empty sub-contract has a represented root.',
    );
    return {
      subject: location.selected,
      coordinates: input.coordinates,
      artifact: { kind: 'flat', nodes } satisfies FlatArtifact,
    };
  };
}

// Equality is exact artifact equality in this canonical example. No semantic
// behavior claims are inferred from the opaque strings in Contract.value.
export function representationEquality<K extends Coordinates>(): RepresentationEquivalence<K> {
  return (left, right) =>
    left.subject === right.subject &&
    isDeepStrictEqual(left.coordinates, right.coordinates) &&
    isDeepStrictEqual(left.artifact, right.artifact);
}

const atNestedAscending = nestedNavigation<typeof nestedAscending>();
const atFlatAscending = flatNavigation<typeof flatAscending>();
const atNestedDescending = nestedNavigation<typeof nestedDescending>();
const atFlatDescending = flatNavigation<typeof flatDescending>();

function observeNested<K extends Coordinates>(): RepresentationObservation<O, K> {
  return (input) => {
    const root = nestedArtifact(input.artifact).root;
    return { kind: 'node', ...valueOf(root), keys: new Set(root.children.map(([key]) => key)) };
  };
}
function observeFlat<K extends Coordinates>(): RepresentationObservation<O, K> {
  return (input) => {
    const nodes = flatArtifact(input.artifact).nodes;
    const root = nodes.find((node) => node.path.length === 0);
    assert.ok(root);
    return {
      ...valueOf(root),
      kind: 'node',
      keys: new Set(nodes.filter((node) => node.path.length === 1).map((node) => node.path[0])),
    };
  };
}

export const flattenLaw = {
  transform: flattenAscending,
  sourceAt: atNestedAscending,
  targetAt: atFlatAscending,
  equivalent: representationEquality<typeof flatAscending>(),
} satisfies NavigationTransparency<O, typeof nestedAscending, typeof flatAscending>;
const reverseNestedLaw = {
  transform: reverseNested,
  sourceAt: atNestedAscending,
  targetAt: atNestedDescending,
  equivalent: representationEquality<typeof nestedDescending>(),
} satisfies NavigationTransparency<O, typeof nestedAscending, typeof nestedDescending>;
const reverseFlatLaw = {
  transform: reverseFlat,
  sourceAt: atFlatAscending,
  targetAt: atFlatDescending,
  equivalent: representationEquality<typeof flatDescending>(),
} satisfies NavigationTransparency<O, typeof flatAscending, typeof flatDescending>;
const flattenDescendingLaw = {
  transform: flattenDescending,
  sourceAt: atNestedDescending,
  targetAt: atFlatDescending,
  equivalent: representationEquality<typeof flatDescending>(),
} satisfies NavigationTransparency<O, typeof nestedDescending, typeof flatDescending>;

const layoutFirstStops = [nestedAscending, flatAscending, flatDescending] as const;
export const layoutFirst = {
  coordinates: layoutFirstStops,
  steps: [flattenAscending, reverseFlat],
} as const satisfies FamilyPath<O, typeof layoutFirstStops>;
const orderFirstStops = [nestedAscending, nestedDescending, flatDescending] as const;
export const orderFirst = {
  coordinates: orderFirstStops,
  steps: [reverseNested, flattenDescending],
} as const satisfies FamilyPath<O, typeof orderFirstStops>;
export const runLayoutFirst: FamilyPathMeaning<O, typeof layoutFirstStops> = (input) =>
  reverseFlat(flattenAscending(input));
export const runOrderFirst: FamilyPathMeaning<O, typeof orderFirstStops> = (input) =>
  flattenDescending(reverseNested(input));
export const compositeLaw = {
  transform: runLayoutFirst,
  sourceAt: atNestedAscending,
  targetAt: atFlatDescending,
  equivalent: representationEquality<typeof flatDescending>(),
} satisfies NavigationTransparency<O, typeof nestedAscending, typeof flatDescending>;

function paths(contract: Contract<O>): ContractPath[] {
  return [[], ...[...contract.children].flatMap(([key, child]) => paths(child).map((path) => [key, ...path]))];
}
function requireLocation<C extends Contract<O>>(root: C, path: ContractPath): ContractLocation<O, C> {
  const selected = locate(root, path);
  assert.equal(selected.kind, 'found');
  return selected.value;
}

// This exercises the equation; a passing finite check is not a universal proof.
export function checkTransparency<From extends Coordinates, To extends Coordinates>(
  law: NavigationTransparency<O, From, To>,
  input: Representation<Contract<O>, From>,
  path: ContractPath,
): void {
  const location = requireLocation(input.subject, path);
  const transformThenSelect = law.targetAt(law.transform(input), location);
  const selectThenTransform = law.transform(law.sourceAt(input, location));
  assert.ok(
    law.equivalent(transformThenSelect, selectThenTransform),
    `Navigation transparency at ${JSON.stringify(path)}`,
  );
}

function checkRepresentationNavigation<K extends Coordinates>(
  input: Representation<Contract<O>, K>,
  at: RepresentationNavigation<O, K>,
  observe: RepresentationObservation<O, K>,
): void {
  assert.ok(representationEquality<K>()(at(input, requireLocation(input.subject, [])), input));
  for (const fullPath of paths(input.subject)) {
    const location = requireLocation(input.subject, fullPath);
    assert.deepEqual(
      observe(at(input, location)),
      {
        ...valueOf(location.selected),
        kind: 'node',
        keys: new Set(location.selected.children.keys()),
      },
      'Every represented local surface agrees with the contract.',
    );
    for (let split = 0; split <= fullPath.length; split++) {
      const prefix = requireLocation(input.subject, fullPath.slice(0, split));
      const suffix = requireLocation(prefix.selected, fullPath.slice(split));
      const direct = at(input, requireLocation(input.subject, fullPath));
      const nested = at(at(input, prefix), suffix);
      assert.ok(representationEquality<K>()(direct, nested), 'Representation navigation composes.');
    }
  }
}

export const rootRepresentation = represent(booleanCellTree);
for (const contract of [booleanCellTree, emptyContract]) {
  const root = represent(contract);
  const a = root;
  const b = flattenAscending(root);
  const c = reverseNested(root);
  const d = runLayoutFirst(root);
  checkRepresentationNavigation(a, atNestedAscending, observeNested());
  checkRepresentationNavigation(b, atFlatAscending, observeFlat());
  checkRepresentationNavigation(c, atNestedDescending, observeNested());
  checkRepresentationNavigation(d, atFlatDescending, observeFlat());
  for (const path of paths(contract)) {
    const direct = contractAt(contract, path);
    for (let split = 0; split <= path.length; split++) {
      const first = contractAt(contract, path.slice(0, split));
      assert.equal(first.kind, 'found');
      if (first.kind === 'found') assert.deepEqual(contractAt(first.value, path.slice(split)), direct);
      // The user's prefix law, exercised for the composite coordinate path:
      // P(at(r, p ++ q)) = at(P(at(r, p)), q).
      const prefix = requireLocation(contract, path.slice(0, split));
      const suffix = requireLocation(prefix.selected, path.slice(split));
      const left = runLayoutFirst(atNestedAscending(a, requireLocation(contract, path)));
      const right = atFlatDescending(runLayoutFirst(atNestedAscending(a, prefix)), suffix);
      assert.ok(representationEquality<typeof flatDescending>()(left, right));
    }
    checkTransparency(flattenLaw, a, path);
    checkTransparency(reverseNestedLaw, a, path);
    checkTransparency(reverseFlatLaw, b, path);
    checkTransparency(flattenDescendingLaw, c, path);
    checkTransparency(compositeLaw, a, path);
    const location = requireLocation(contract, path);
    const selected = atNestedAscending(a, location);
    assert.ok(representationEquality<typeof flatDescending>()(runLayoutFirst(selected), runOrderFirst(selected)));
  }
  assert.ok(representationEquality<typeof flatDescending>()(runLayoutFirst(root), runOrderFirst(root)));
}

assert.equal(contractAt(emptyContract, []).kind, 'found');
assert.equal(contractAt(emptyContract, ['missing']).kind, 'missing');
assert.equal(contractAt(booleanCellTree, ['operations', 'missing', 'result']).kind, 'missing');
assert.equal(contractAt(booleanCellTree, ['operations', 'read', 'arguments']).kind, 'found');

// Partial navigation uses Option/bind: a missing prefix cannot be resumed.
for (const path of [['missing'], ['operations', 'missing', 'result']]) {
  for (let split = 0; split <= path.length; split++) {
    const prefix = contractAt(booleanCellTree, path.slice(0, split));
    const nested = prefix.kind === 'missing' ? prefix : contractAt(prefix.value, path.slice(split));
    assert.deepEqual(nested, contractAt(booleanCellTree, path));
  }
}

// Counterexample: retaining subject and endpoint indices is insufficient. This
// same-signature function drops flat descendant nodes and violates transparency.
const dropDescendants: TransformationFamily<O, typeof nestedAscending, typeof flatAscending> = (input) => {
  const output = flattenAscending(input);
  return {
    ...output,
    artifact: { kind: 'flat', nodes: flatArtifact(output.artifact).nodes.slice(0, 1) } satisfies FlatArtifact,
  };
};
assert.throws(() =>
  checkTransparency({ ...flattenLaw, transform: dropDescendants }, rootRepresentation, ['operations']),
);
assert.throws(() =>
  atNestedAscending(rootRepresentation, { root: booleanCellTree, path: ['operations'], selected: emptyContract }),
);

// A different counterexample COMMUTES with navigation but changes every local
// value. Faithfulness, not the commuting square alone, must rule it out.
const replaceValues: TransformationFamily<O, typeof nestedAscending, typeof flatAscending> = (input) => {
  const output = flattenAscending(input);
  return {
    ...output,
    artifact: {
      kind: 'flat',
      nodes: flatArtifact(output.artifact).nodes.map((node) => ({ ...node, value: 'Changed' })),
    } satisfies FlatArtifact,
  };
};
for (const path of paths(booleanCellTree)) {
  checkTransparency({ ...flattenLaw, transform: replaceValues }, rootRepresentation, path);
}
assert.throws(() => checkRepresentationNavigation(replaceValues(rootRepresentation), atFlatAscending, observeFlat()));

console.log({
  subject: booleanCellTree.value,
  contractPathsChecked: paths(booleanCellTree).length,
  endpoint: flatDescending,
  twoCoordinatePathsAgree: true,
  navigationTransparency: 'checked at every node and every path split in the example',
  invalidTransformAndLocation: 'rejected by the law checks',
});
