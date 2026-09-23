/**
 * Instances of the opaque coordinate model. TypeScript is the metalanguage.
 * The strings below describe Go/TypeScript artifacts; the functions map these
 * example descriptions. They are finite examples, not Nightseam generators.
 *
 * This example takes S to be SHAPE ONLY. Its artifact interpretations are
 * stipulated here; type-checking does not parse source or prove equivalence.
 * Run from the repository root: pnpm --filter @nightseam/theory test
 * @see [Coordinates and cells](../foundations.md#6-coordinates-and-cells)
 * @see [Elementary transformations and paths](../foundations.md#7-elementary-transformations-and-coordinate-paths)
 * @see [Evidence map](../cross-references.md)
 */
import assert from 'node:assert/strict';
import type {
  AxisElementEquality,
  ChangedAxis,
  CoordinateEquality,
  Coordinates,
  CoordinatesEqual,
  Path,
  PathMeaning,
  Representation,
  Transformation,
  TransformationInstance,
  TransformationSignature,
} from '../model.ts';

// A concrete interpretation of the equality operations. No axis meaning is
// inspected. SameValueZero also makes equality reflexive for numeric IDs.
export const equalAxisElements: AxisElementEquality = (left, right) => left === right || Object.is(left, right);
export const equalCoordinates: CoordinateEquality = (left, right) => {
  const axes = Object.keys(left);
  return (
    axes.length === Object.keys(right).length &&
    axes.every((axis) => Object.hasOwn(right, axis) && equalAxisElements(left[axis], right[axis]))
  );
};

export const booleanCell = {
  id: 'BooleanCell',
  operations: {
    read: { inputs: [], output: 'Bool' },
    write: { inputs: ['Bool'], output: 'Unit' },
  },
} as const;
type S = typeof booleanCell;

// The axis schema is chosen here, outside the core model. "rank:key" addresses
// encode positions and keys. The empty execution coordinate is still a value.
export const goShapeAt = {
  '0:role': 'shape',
  '0:form': 'syntax',
  '1:language': 'go',
  '2:execution': null,
} as const satisfies Coordinates;

export const goTypeAt = { ...goShapeAt, '0:role': 'model-type' } as const satisfies Coordinates;
export const tsShapeAt = { ...goShapeAt, '1:language': 'typescript' } as const satisfies Coordinates;
export const tsTypeAt = { ...goTypeAt, '1:language': 'typescript' } as const satisfies Coordinates;
export const goMeaningAt = { ...goTypeAt, '0:form': 'semantics' } as const satisfies Coordinates;

// Shape descriptors expressed as source, followed by native type source.
// Shape/Operation and defineShape are illustrative declaration constructs.
export const goShape = {
  subject: booleanCell,
  coordinates: goShapeAt,
  artifact: `Shape{Name: "BooleanCell", Operations: []Operation{
  {Name: "read", Inputs: nil, Output: "Bool"},
  {Name: "write", Inputs: []string{"Bool"}, Output: "Unit"},
}}`,
} as const satisfies Representation<S, typeof goShapeAt>;

export const goType = {
  subject: booleanCell,
  coordinates: goTypeAt,
  artifact: 'type BooleanCell interface { Read() bool; Write(value bool) }',
} as const satisfies Representation<S, typeof goTypeAt>;

export const tsShape = {
  subject: booleanCell,
  coordinates: tsShapeAt,
  artifact: `defineShape("BooleanCell", {
  read: { inputs: [], output: "Bool" },
  write: { inputs: ["Bool"], output: "Unit" },
})`,
} as const satisfies Representation<S, typeof tsShapeAt>;

export const tsType = {
  subject: booleanCell,
  coordinates: tsTypeAt,
  artifact: 'interface BooleanCell { read(): boolean; write(value: boolean): void }',
} as const satisfies Representation<S, typeof tsTypeAt>;

export const goMeaning = {
  subject: booleanCell,
  coordinates: goMeaningAt,
  artifact: {
    description: 'The native Go method-interface meaning.',
    correspondence: { read: 'Read', write: 'Write', Bool: 'bool', Unit: 'no result' },
  },
} as const satisfies Representation<S, typeof goMeaningAt>;

// Different artifacts may occupy the SAME cell. Coordinates are not object IDs.
export const formattedGoType = {
  ...goType,
  artifact: 'type BooleanCell interface {\n  Read() bool\n  Write(value bool)\n}',
} as const satisfies Representation<S, typeof goTypeAt>;

// Each function below is an inhabitant of a single-axis transformation type.
// All inputs already represent our fixed S, so selecting a fixed output for S
// is a legitimate small example of a shape-preserving map between these cells.
export const generateGoType: Transformation<S, typeof goShapeAt, typeof goTypeAt> = (input) => ({
  ...goType,
  subject: input.subject,
});

export const translateType: Transformation<S, typeof goTypeAt, typeof tsTypeAt> = (input) => ({
  ...tsType,
  subject: input.subject,
});

export const translateShape: Transformation<S, typeof goShapeAt, typeof tsShapeAt> = (input) => ({
  ...tsShape,
  subject: input.subject,
});

export const generateTypeScriptType: Transformation<S, typeof tsShapeAt, typeof tsTypeAt> = (input) => ({
  ...tsType,
  subject: input.subject,
});

export const interpretGoType: Transformation<S, typeof goTypeAt, typeof goMeaningAt> = (input) => ({
  ...goMeaning,
  subject: input.subject,
});

// Make the endpoint tuple a model object in its own right. Both functions below
// have exactly this signature, although they produce different source strings.
export const goTypeGenerationSignature = {
  subject: booleanCell,
  endpoints: [goShapeAt, goTypeAt],
} as const satisfies TransformationSignature<S, typeof goShapeAt, typeof goTypeAt>;

export const goTypeGenerators = [
  {
    id: 'compact-go-type-generator',
    signature: goTypeGenerationSignature,
    apply: generateGoType,
  },
  {
    id: 'multiline-go-type-generator',
    signature: goTypeGenerationSignature,
    apply: (input) => ({ ...formattedGoType, subject: input.subject }),
  },
] as const satisfies readonly TransformationInstance<S, typeof goShapeAt, typeof goTypeAt>[];

// Two distinct paths between the same endpoints. Each changes role and language,
// in a different order. Each intermediate coordinate has a representation here.
const throughGoStops = [goShapeAt, goTypeAt, tsTypeAt] as const;
export const throughGo = {
  subject: booleanCell,
  coordinates: throughGoStops,
  steps: [generateGoType, translateType],
} as const satisfies Path<S, typeof throughGoStops>;

const throughTypeScriptStops = [goShapeAt, tsShapeAt, tsTypeAt] as const;
export const throughTypeScript = {
  subject: booleanCell,
  coordinates: throughTypeScriptStops,
  steps: [translateShape, generateTypeScriptType],
} as const satisfies Path<S, typeof throughTypeScriptStops>;

// Path interpretation is function composition in the metalanguage.
export const runThroughGo: PathMeaning<S, typeof throughGoStops> = (input) =>
  throughGo.steps[1](throughGo.steps[0](input));
export const runThroughTypeScript: PathMeaning<S, typeof throughTypeScriptStops> = (input) =>
  throughTypeScript.steps[1](throughTypeScript.steps[0](input));

const identityStops = [goShapeAt] as const;
export const identityPath = {
  subject: booleanCell,
  coordinates: identityStops,
  steps: [],
} as const satisfies Path<S, typeof identityStops>;
export const runIdentity: PathMeaning<S, typeof identityStops> = (input) => input;

// Empty higher coordinates participate in the same comparison rule. Filling
// this slot would be one change; it does not establish that a lawful map exists.
export const initializedGoAt = { ...goMeaningAt, '2:execution': 'initialized' } as const satisfies Coordinates;
export const executionAxis: ChangedAxis<typeof goMeaningAt, typeof initializedGoAt> = '2:execution';

// These types are empty: they cannot contain elementary transformations.
type MustBeNever<T extends never> = T;
export type NoZeroAxisStep = MustBeNever<Transformation<S, typeof goTypeAt, typeof goTypeAt>>;
export type NoTwoAxisStep = MustBeNever<Transformation<S, typeof goShapeAt, typeof tsTypeAt>>;
export type NoUnspecifiedPoint = MustBeNever<Transformation<S, Coordinates, typeof goTypeAt>>;
export const sameCell: CoordinatesEqual<typeof goTypeAt, typeof formattedGoType.coordinates> = true;
export const differentCell: CoordinatesEqual<typeof goTypeAt, typeof tsTypeAt> = false;

// A few checks on the concrete example, not a general proof of preservation.
const resultThroughGo = runThroughGo(goShape);
const resultThroughTypeScript = runThroughTypeScript(goShape);
assert.equal(resultThroughGo.subject, booleanCell);
assert.equal(resultThroughTypeScript.subject, booleanCell);
assert.deepEqual(resultThroughGo, resultThroughTypeScript);
assert.deepEqual(resultThroughGo, tsType);
assert.equal(runIdentity(goShape), goShape);
assert.deepEqual(interpretGoType(formattedGoType), goMeaning);
assert.equal(equalCoordinates(goTypeAt, { ...goTypeAt }), true);
assert.equal(equalCoordinates(goTypeAt, tsTypeAt), false);
assert.equal(equalCoordinates({ first: 'a', second: 'b' }, { first: 'b', second: 'a' }), false);
assert.equal(equalCoordinates({}, { '2:execution': null }), false);
assert.equal(goTypeGenerators[0].signature, goTypeGenerators[1].signature);
const compact = goTypeGenerators[0].apply(goShape);
const multiline = goTypeGenerators[1].apply(goShape);
assert.equal(compact.subject, multiline.subject);
assert.equal(equalCoordinates(compact.coordinates, multiline.coordinates), true);
assert.notEqual(compact.artifact, multiline.artifact);

console.log({
  subject: booleanCell.id,
  throughGo: throughGo.coordinates,
  throughTypeScript: throughTypeScript.coordinates,
  identitySteps: identityPath.steps.length,
  sameSignatureDifferentFunctions: goTypeGenerators.map((generator) => generator.id),
  bothPathsProduce: resultThroughGo.artifact,
});
