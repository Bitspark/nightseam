// Compile-only assertions, including intentional failures. Do not execute this file.
import type {
  ChangedAxis,
  Shape,
  ShapeRepresentationMap,
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
  AdapterSemantics,
  Contract,
  ContractImplementation,
  ModelInstance,
  ModelType,
  ModelTypeSyntax,
  Syntax,
} from './model.ts';
import {
  adapterGeneration,
  cellContract,
  functionCell,
  functionType,
  generateAdapter,
  contractSyntax,
  methodSyntax,
  methodType,
  surfaceType,
  trueCell,
  type CellMachine,
  type MethodValue,
} from './examples/behavior.ts';
import {
  booleanCell,
  goShapeAt,
  goTypeAt,
  goType,
  tsTypeAt,
  generateGoType,
  translateShape,
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
type NoTwoAxisSignature = Assert<Equal<TransformationSignature<S, typeof goShapeAt, typeof tsTypeAt>, never>>;

// @ts-expect-error Two-axis composite is not an elementary transformation.
const invalidTwoAxis: Transformation<S, typeof goShapeAt, typeof tsTypeAt> = runThroughGo;
// @ts-expect-error Identity changes zero axes.
const invalidIdentity: Transformation<S, typeof goShapeAt, typeof goShapeAt> = runIdentity;

const brokenPath: Path<S, typeof throughGo.coordinates> = {
  ...throughGo,
  // @ts-expect-error Step two requires goTypeAt, but this function requires goShapeAt.
  steps: [generateGoType, translateShape],
};

const otherSubject = { ...booleanCell, id: 'DifferentSubject' } as const;
const wrongSubjectMap = (input: Representation<S, typeof goShapeAt>) => ({
  ...goType,
  subject: otherSubject,
});
// @ts-expect-error A valid one-axis signature cannot change S.
const changesSubject: Transformation<S, typeof goShapeAt, typeof goTypeAt> = wrongSubjectMap;

const mismatchedInstance: TransformationInstance<S, typeof goShapeAt, typeof goTypeAt> = {
  id: 'wrong-signature',
  signature: goTypeGenerationSignature,
  // @ts-expect-error A function inhabiting another endpoint pair cannot fill this signature.
  apply: translateShape,
};

const unboundedRoute: Path<S, readonly [Coordinates, ...Coordinates[]]> = {
  subject: booleanCell,
  // @ts-expect-error Finite path links cannot be checked from an unbounded coordinate array.
  coordinates: [goShapeAt],
  steps: [],
};

type KA = { form: 'nested'; order: 'ascending' };
type KB = { form: 'flat'; order: 'ascending' };
type KC = { form: 'flat'; order: 'descending' };
type NoTwoAxisFamily = Assert<Equal<TransformationFamily<string, KA, KC>, never>>;
type NoIdentityFamily = Assert<Equal<TransformationFamily<string, KA, KA>, never>>;
declare const arbitraryShape: Shape<string>;
const subjectReplacingFamily: ShapeRepresentationMap<string, KA, KB> = (input) => ({
  ...input,
  coordinates: { form: 'flat', order: 'ascending' } as const,
  // @ts-expect-error A family must preserve the particular C, not replace it with any Shape.
  subject: arbitraryShape,
});
// @ts-expect-error Navigation returns Selected, not Root, even though both are shapes.
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
// @ts-expect-error Closed shapes do not admit a generic branch.
const holeInClosedShape: Shape<string> = { kind: 'generic', id: 'g0' };
// @ts-expect-error A whole-shape hole has no local children of its own.
const holeWithChildren: Generic<'g0'> = { kind: 'generic', id: 'g0', children: new Map() };
// @ts-expect-error The replacement's free generic must belong to H.
const wrongTargetContext: Substitution<string, 'g0', 'h0'> = () => ({ kind: 'generic', id: 'unbound' });
// @ts-expect-error Substitution supplies a whole shape, not just an opaque local value.
const localValueInsteadOfShape: Substitution<string, 'g0', never> = () => 'Bool';
const unfinishedShape: Shape<string, 'h0'> = { kind: 'generic', id: 'h0' };
// @ts-expect-error A closing substitution cannot leave a target generic.
const unfinishedClosingSubstitution: Substitution<string, 'g0', never> = () => unfinishedShape;

const correctSelectedType: ModelTypeSyntax<typeof methodType> = adapterGeneration.modelType;
// @ts-expect-error A function-record value is not an instance of the chosen method interface.
const wrongNativeInstance: ModelInstance<typeof methodType> = functionCell;
const wrongTypeSyntax: ModelTypeSyntax<typeof methodType> = {
  ...methodSyntax,
  // @ts-expect-error Same shape and language do not identify the same native realization.
  subject: functionType,
};
// @ts-expect-error Syntax denoting T is not syntax denoting the contract C.
const typeAsContract: Syntax<typeof cellContract, 'go'> = methodSyntax;
// @ts-expect-error Shape trees have neither a behavior domain nor a specification.
const treeAsContract: Contract<Shape<string>, CellMachine> = arbitraryShape;
// @ts-expect-error B for machines cannot be silently reused for a different behavior domain.
const wrongBehaviorDomain: Contract<typeof booleanCell, number> = cellContract;

declare const otherShapeType: ModelType<{ readonly id: 'OtherShape' }, CellMachine, 'go', MethodValue>;
type NoWrongShapeAdapter = Assert<Equal<AdapterSemantics<typeof methodType, typeof otherShapeType>, never>>;
declare const otherBehaviorType: ModelType<typeof booleanCell, number, 'go', MethodValue>;
type NoWrongBehaviorAdapter = Assert<Equal<AdapterSemantics<typeof methodType, typeof otherBehaviorType>, never>>;

generateAdapter({
  contract: contractSyntax,
  modelType: {
    ...methodSyntax,
    // @ts-expect-error Adapter generation must use the selected native T supplied as input.
    subject: functionType,
  },
});
const wrongOutputAdapter: AdapterSemantics<typeof methodType, typeof surfaceType> = {
  sourceType: methodType,
  targetType: surfaceType,
  // @ts-expect-error The adapter result must belong to the declared target realization.
  adapt: () => functionCell,
};
// @ts-expect-error Native structural compatibility alone does not supply satisfaction evidence.
const missingSatisfaction: ContractImplementation<typeof methodType> = {
  instance: trueCell,
};
