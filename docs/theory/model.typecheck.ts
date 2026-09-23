// Compile-only assertions, including intentional failures. Do not execute this file.
import type {
  ChangedAxis,
  Contract,
  ContractRepresentationMap,
  Coordinates,
  CoordinatesEqual,
  Path,
  Representation,
  Transformation,
  TransformationInstance,
  TransformationSignature,
  TransformationFamily,
  RepresentationNavigation,
  FamilyPath,
  Generic,
  Substitution,
} from './model.ts';
import {
  booleanCell,
  goContractAt,
  goTypeAt,
  goType,
  tsTypeAt,
  generateGoType,
  translateContract,
  throughGo,
  runThroughGo,
  runIdentity,
  goTypeGenerationSignature,
} from './examples/coordinates.ts';

type S = typeof booleanCell;
type Assert<T extends true> = T;
type Equal<A, B> = [A] extends [B] ? ([B] extends [A] ? true : false) : false;

type One = Assert<Equal<ChangedAxis<{ x: 'a'; y: 0 }, { x: 'b'; y: 0 }>, 'x'>>;
type Added = Assert<Equal<ChangedAxis<{}, { x: 'a' }>, 'x'>>;
type Removed = Assert<Equal<ChangedAxis<{ x: 'a' }, {}>, 'x'>>;
type EmptyHigher = Assert<Equal<ChangedAxis<{ x: null }, { x: 'a' }>, 'x'>>;
type NoChange = Assert<Equal<ChangedAxis<{ x: true }, { x: true }>, never>>;
type Two = Assert<Equal<ChangedAxis<{ x: 'a'; y: 0 }, { x: 'b'; y: 1 }>, never>>;
type Three = Assert<Equal<ChangedAxis<{ x: 'a'; y: 0; z: true }, { x: 'b'; y: 1; z: false }>, never>>;
type BroadValue = Assert<Equal<ChangedAxis<{ x: string }, { x: 'a' }>, never>>;
type UnionValue = Assert<Equal<ChangedAxis<{ x: 'a' | 'b' }, { x: 'a' }>, never>>;
type UnionPoint = Assert<Equal<ChangedAxis<{ x: 'a' } | { y: 'b' }, { x: 'c' }>, never>>;
type BroadAxis = Assert<Equal<ChangedAxis<Record<string, 'a'>, Record<string, 'b'>>, never>>;
type NumericIndex = Assert<Equal<ChangedAxis<Record<number, 'a'>, Record<number, 'b'>>, never>>;
// Axis IDs are strings. Numeric/symbol keys cannot quietly disagree with Object.keys equality.
type NumericLiteralKey = Assert<Equal<ChangedAxis<{ 1: 'a' }, { 1: 'b' }>, never>>;
declare const symbolAxis: unique symbol;
type SymbolLiteralKey = Assert<Equal<ChangedAxis<{ [symbolAxis]: 'a' }, { [symbolAxis]: 'b' }>, never>>;
type EqualPoints = Assert<Equal<CoordinatesEqual<{ a: 'x'; b: null }, { b: null; a: 'x' }>, true>>;
type UnequalPoints = Assert<Equal<CoordinatesEqual<{ a: 'x' }, { a: 'y' }>, false>>;
type MissingIsDistinct = Assert<Equal<CoordinatesEqual<{}, { a: null }>, false>>;
type UnknownPointEquality = Assert<Equal<CoordinatesEqual<Coordinates, Coordinates>, boolean>>;
type NoTwoAxisSignature = Assert<Equal<TransformationSignature<S, typeof goContractAt, typeof tsTypeAt>, never>>;

// @ts-expect-error Two-axis composite is not an elementary transformation.
const invalidTwoAxis: Transformation<S, typeof goContractAt, typeof tsTypeAt> = runThroughGo;
// @ts-expect-error Identity changes zero axes.
const invalidIdentity: Transformation<S, typeof goContractAt, typeof goContractAt> = runIdentity;

const brokenPath: Path<S, typeof throughGo.coordinates> = {
  ...throughGo,
  // @ts-expect-error Step two requires goTypeAt, but this function requires goContractAt.
  steps: [generateGoType, translateContract],
};

const otherSubject = { ...booleanCell, id: 'DifferentSubject' } as const;
const wrongSubjectMap = (input: Representation<S, typeof goContractAt>) => ({
  ...goType,
  subject: otherSubject,
});
// @ts-expect-error A valid one-axis signature cannot change S.
const changesSubject: Transformation<S, typeof goContractAt, typeof goTypeAt> = wrongSubjectMap;

const mismatchedInstance: TransformationInstance<S, typeof goContractAt, typeof goTypeAt> = {
  id: 'wrong-signature',
  signature: goTypeGenerationSignature,
  // @ts-expect-error A function inhabiting another endpoint pair cannot fill this signature.
  apply: translateContract,
};

const unboundedRoute: Path<S, readonly [Coordinates, ...Coordinates[]]> = {
  subject: booleanCell,
  // @ts-expect-error Finite path links cannot be checked from an unbounded coordinate array.
  coordinates: [goContractAt],
  steps: [],
};

type KA = { form: 'nested'; order: 'ascending' };
type KB = { form: 'flat'; order: 'ascending' };
type KC = { form: 'flat'; order: 'descending' };
type NoTwoAxisFamily = Assert<Equal<TransformationFamily<string, KA, KC>, never>>;
type NoIdentityFamily = Assert<Equal<TransformationFamily<string, KA, KA>, never>>;
declare const arbitraryContract: Contract<string>;
const subjectReplacingFamily: ContractRepresentationMap<string, KA, KB> = (input) => ({
  ...input,
  coordinates: { form: 'flat', order: 'ascending' } as const,
  // @ts-expect-error A family must preserve the particular C, not replace it with any Contract.
  subject: arbitraryContract,
});
// @ts-expect-error Navigation returns Selected, not Root, even though both are contracts.
const rootReturningNavigation: RepresentationNavigation<string, KA> = (input, location) => input;
declare const toFlat: TransformationFamily<string, KA, KB>;
declare const toDescending: TransformationFamily<string, KB, KC>;
declare const ka: KA;
declare const kb: KB;
declare const kc: KC;
const validFamilyPath: FamilyPath<string, readonly [KA, KB, KC]> = {
  coordinates: [ka, kb, kc],
  steps: [toFlat, toDescending],
};
const brokenFamilyPath: FamilyPath<string, readonly [KA, KB, KC]> = {
  coordinates: [ka, kb, kc],
  // @ts-expect-error The second step starts at KB, so another KA -> KB step cannot fill it.
  steps: [toFlat, toFlat],
};

type ClosedHasNoHoles = Assert<Equal<Generic<never>, never>>;
// @ts-expect-error Closed contracts do not admit a generic branch.
const holeInClosedContract: Contract<string> = { kind: 'generic', id: 'g0' };
// @ts-expect-error A whole-contract hole has no local children of its own.
const holeWithChildren: Generic<'g0'> = { kind: 'generic', id: 'g0', children: new Map() };
// @ts-expect-error The replacement's free generic must belong to H.
const wrongTargetContext: Substitution<string, 'g0', 'h0'> = () => ({ kind: 'generic', id: 'unbound' });
// @ts-expect-error Substitution supplies a whole contract, not just an opaque local value.
const localValueInsteadOfContract: Substitution<string, 'g0', never> = () => 'Bool';
const unfinishedContract: Contract<string, 'h0'> = { kind: 'generic', id: 'h0' };
// @ts-expect-error A closing substitution cannot leave a target generic.
const unfinishedClosingSubstitution: Substitution<string, 'g0', never> = () => unfinishedContract;
