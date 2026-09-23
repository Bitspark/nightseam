/**
 * A metamodel, expressed entirely as types. X and the meanings of the axes
 * are supplied by an interpretation. The coordinate core assigns no role names;
 * the semantic layer distinguishes contracts, realizations and implementations.
 *
 * Representation<X, K> means a representation OF X, not merely one tagged X.
 * Preserving X (including any chosen behavioral laws) is a semantic obligation.
 * TypeScript checks the indices and adjacency, not the meaning of an artifact.
 * @see ./reference.md — definitions, laws, and evidence for every exported type.
 * @see ./foundations.md#7-elementary-transformations-and-coordinate-paths
 */

// IDs are opaque: the core compares identity without interpreting their meaning.
// Element IDs are scoped to their axis; the same ID on another axis is unrelated.
// Use concrete literals (`as const`), not unions or `string`.
// Axis keys may encode ranked addresses such as "0:form" and "1:language".
export type AxisId = string;
export type AxisElementId = string | number | boolean;
export type EmptyCoordinate = null;
export type CoordinateValue = AxisElementId | EmptyCoordinate;
export type Coordinates = Readonly<Record<AxisId, CoordinateValue>>;

// Required meaning: equality of IDs within the SAME axis, including empty slots.
export type AxisElementEquality = (left: CoordinateValue, right: CoordinateValue) => boolean;

// Required meaning: identical key sets and equal elements at every matching key.
// A missing key is distinct from a present key whose value is explicitly null.
export type CoordinateEquality = (left: Coordinates, right: Coordinates) => boolean;

export interface Representation<X, K extends Coordinates> {
  readonly subject: X;
  readonly coordinates: K;
  readonly artifact: unknown;
}

// An unrestricted map can interpret a path that changes zero or several axes.
export type RepresentationMap<X, From extends Coordinates, To extends Coordinates> = (
  input: Representation<X, From>,
) => Representation<X, To>;

type Equal<A, B> = [A] extends [B] ? ([B] extends [A] ? true : false) : false;
type IsUnion<T, Whole = T> = T extends Whole ? ([Whole] extends [T] ? false : true) : never;
type At<K, Axis extends PropertyKey> = Axis extends keyof K ? K[Axis] : never;

// Broad, patterned, branded or union-valued coordinates can describe families.
// A singleton string/number makes a REQUIRED Record key; an index domain admits
// the empty record. This also rejects `go:${string}` and patterned axis keys.
type IsLiteral<T> = [T] extends [never]
  ? false
  : true extends IsUnion<T>
    ? false
    : T extends string | number
      ? {} extends Record<T, unknown>
        ? false
        : true
      : Equal<T, true> extends true
        ? true
        : Equal<T, false> extends true
          ? true
          : Equal<T, null>;

type IsPoint<K extends Coordinates> =
  true extends IsUnion<K>
    ? false
    : [keyof K] extends [AxisId]
      ? string extends keyof K
        ? false
        : false extends {
              [Axis in keyof K]-?: IsLiteral<Axis> extends true ? IsLiteral<K[Axis]> : false;
            }[keyof K]
          ? false
          : true
      : false;

// Adding or removing a key also counts as changing that axis.
export type DifferingAxes<From extends Coordinates, To extends Coordinates> = {
  [Axis in keyof From | keyof To]: Equal<At<From, Axis>, At<To, Axis>> extends true ? never : Axis;
}[keyof From | keyof To];

// Static equality for concrete points. With families, equality is not yet known.
export type CoordinatesEqual<Left extends Coordinates, Right extends Coordinates> =
  IsPoint<Left> extends true
    ? IsPoint<Right> extends true
      ? [DifferingAxes<Left, Right>] extends [never]
        ? true
        : false
      : boolean
    : boolean;

// The unique changed axis, or never for zero/multiple changes or nonliteral points.
export type ChangedAxis<From extends Coordinates, To extends Coordinates> =
  IsPoint<From> extends true
    ? IsPoint<To> extends true
      ? [DifferingAxes<From, To>] extends [never]
        ? never
        : true extends IsUnion<DifferingAxes<From, To>>
          ? never
          : DifferingAxes<From, To>
      : never
    : never;

/**
 * One elementary function, with the same X at both ends and exactly one axis
 * changed. This type is empty (`never`) when the endpoints are not adjacent.
 * Every inhabitant is required to preserve X; the signature alone is no proof
 * of shape isomorphism or behavioral preservation.
 */
export type Transformation<X, From extends Coordinates, To extends Coordinates> = [ChangedAxis<From, To>] extends [
  never,
]
  ? never
  : RepresentationMap<X, From, To>;

// The first-class signature: for fixed X, its identity is the ordered endpoint
// tuple. It determines the transformation TYPE, without selecting an inhabitant.
export type TransformationSignature<X, From extends Coordinates, To extends Coordinates> = [
  ChangedAxis<From, To>,
] extends [never]
  ? never
  : {
      readonly subject: X;
      readonly endpoints: readonly [from: From, to: To];
    };

// Several named functions may inhabit the same signature. `apply` is the
// function itself; its id is independent of the signature's endpoint identity.
export interface TransformationInstance<X, From extends Coordinates, To extends Coordinates> {
  readonly id: string;
  readonly signature: TransformationSignature<X, From, To>;
  readonly apply: Transformation<X, From, To>;
}

// A path lists all visited coordinates. One stop and zero steps is identity.
export type Stops = readonly [Coordinates, ...Coordinates[]];

export type PathSteps<X, Route extends Stops> = Route extends readonly [
  infer From extends Coordinates,
  infer To extends Coordinates,
  ...infer Rest extends Coordinates[],
]
  ? readonly [Transformation<X, From, To>, ...PathSteps<X, readonly [To, ...Rest]>]
  : readonly [];

export interface Path<X, Route extends Stops> {
  readonly subject: X;
  // A finite tuple makes every link checkable. An unbounded array loses that evidence.
  readonly coordinates: number extends Route['length'] ? never : Route;
  readonly steps: PathSteps<X, Route>;
}

type Last<Route extends Stops> = Route extends readonly [...Coordinates[], infer K extends Coordinates] ? K : never;

// Interpreting a path composes its steps; it need not be an elementary step.
export type PathMeaning<X, Route extends Stops> = RepresentationMap<X, Route[0], Last<Route>>;

// Shape structure specializes the previously opaque subject X. Shape keys
// are independent of coordinate-axis IDs, even though both use opaque strings.
export type ShapeKey = string;
export type ShapePath = readonly ShapeKey[];

export type GenericId = string;

/** A hole stands for an ENTIRE shape; it has no local value or children. */
export type Generic<G extends GenericId> = [G] extends [never] ? never : { readonly kind: 'generic'; readonly id: G };

/** An immutable, finite, acyclic node. Children have unique keys; order is immaterial. */
export interface ShapeNode<O, G extends GenericId = never> {
  readonly kind: 'node';
  readonly value?: O;
  readonly children: ReadonlyMap<ShapeKey, Shape<O, G>>;
}

/** G is the allowed free-hole context. G = never recovers closed shapes. */
export type Shape<O = unknown, G extends GenericId = never> = ShapeNode<O, G> | Generic<G>;

// An existing node without a value or children is still Found, not Missing.
export type Selection<T> = { readonly kind: 'found'; readonly value: T } | { readonly kind: 'missing' };

/** Required laws: at(S, []) = found(S); at(S, p ++ q) = bind(at(S, p), at(_, q)). */
export type ShapeNavigation<O, G extends GenericId = never> = (
  shape: Shape<O, G>,
  path: ShapePath,
) => Selection<Shape<O, G>>;

/**
 * A location witnesses that found(selected) = at(root, path). Construction must establish
 * this equation. TypeScript relates the subject types but cannot prove the lookup.
 */
export interface ShapeLocation<
  O,
  Root extends Shape<O, G>,
  G extends GenericId = never,
  Selected extends Shape<O, G> = Shape<O, G>,
> {
  readonly root: Root;
  readonly path: ShapePath;
  readonly selected: Selected;
}

/**
 * Given a valid location, select the corresponding representation at the SAME
 * coordinate K. Navigation may retain the context needed to interpret a fragment.
 * Its result's subject is the selected sub-shape, not the original root.
 */
export interface RepresentationNavigation<O, K extends Coordinates, G extends GenericId = never> {
  <Root extends Shape<O, G>, Selected extends Shape<O, G>>(
    input: Representation<Root, K>,
    location: ShapeLocation<O, Root, G, Selected>,
  ): Representation<Selected, K>;
}

/** The observable shape surface at ONE node. Child order is not semantic. */
export interface ShapeNodeSurface<O> {
  readonly kind: 'node';
  readonly value?: O;
  readonly keys: ReadonlySet<ShapeKey>;
}

export type ShapeSurface<O, G extends GenericId = never> = ShapeNodeSurface<O> | Generic<G>;

/**
 * Observe the represented artifact, not just its subject field. Required law:
 * observe(at_K(r, p)) = surface(at(r.subject, p)) for every valid path p.
 * Equality of opaque values is supplied by the model's user.
 */
export interface RepresentationObservation<O, K extends Coordinates, G extends GenericId = never> {
  <S extends Shape<O, G>>(input: Representation<S, K>): ShapeSurface<O, G>;
}

/** An equivalence relation, respected by navigation and admitted transformations. */
export interface RepresentationEquivalence<K extends Coordinates> {
  <X>(left: Representation<X, K>, right: Representation<X, K>): boolean;
}

// A polymorphic map supplies a component for EVERY shape in the chosen domain.
// The domain here is finite Shape<O, G> trees, and is closed under navigation.
export interface ShapeRepresentationMap<
  O,
  From extends Coordinates,
  To extends Coordinates,
  G extends GenericId = never,
> {
  <S extends Shape<O, G>>(input: Representation<S, From>): Representation<S, To>;
}

export type TransformationFamily<O, From extends Coordinates, To extends Coordinates, G extends GenericId = never> = [
  ChangedAxis<From, To>,
] extends [never]
  ? never
  : ShapeRepresentationMap<O, From, To, G>;

export type FamilyPathSteps<O, Route extends Stops, G extends GenericId = never> = Route extends readonly [
  infer From extends Coordinates,
  infer To extends Coordinates,
  ...infer Rest extends Coordinates[],
]
  ? readonly [TransformationFamily<O, From, To, G>, ...FamilyPathSteps<O, readonly [To, ...Rest], G>]
  : readonly [];

export interface FamilyPath<O, Route extends Stops, G extends GenericId = never> {
  readonly coordinates: number extends Route['length'] ? never : Route;
  readonly steps: FamilyPathSteps<O, Route, G>;
}

export type FamilyPathMeaning<O, Route extends Stops, G extends GenericId = never> = ShapeRepresentationMap<
  O,
  Route[0],
  Last<Route>,
  G
>;

/**
 * Operations specifying the navigation-transparency law, for a step OR a path:
 *
 *   targetAt(transform(input), location)
 *     ≈ transform(sourceAt(input, location))
 *
 * The equation is required for every lawful input and valid location. Storing
 * this record is not a proof. The examples exercise both sides on real encodings.
 */
export interface NavigationTransparency<
  O,
  From extends Coordinates,
  To extends Coordinates,
  G extends GenericId = never,
> {
  readonly transform: ShapeRepresentationMap<O, From, To, G>;
  readonly sourceAt: RepresentationNavigation<O, From, G>;
  readonly targetAt: RepresentationNavigation<O, To, G>;
  readonly equivalent: RepresentationEquivalence<To>;
}

/** A total, simultaneous assignment of source holes to shapes over target holes. */
export type Substitution<O, G extends GenericId, H extends GenericId> = (id: G) => Shape<O, H>;

/** Substitute once: a replacement is returned as-is, not traversed under the same assignment. */
export interface ShapeSubstitution<O> {
  <G extends GenericId, H extends GenericId>(template: Shape<O, G>, bindings: Substitution<O, G, H>): Shape<O, H>;
}

/** A semantic witness: result = template[bindings], preserving the particular result subject. */
export interface SubstitutionApplication<
  O,
  G extends GenericId,
  H extends GenericId,
  S extends Shape<O, G>,
  D extends Shape<O, H>,
> {
  readonly template: S;
  readonly bindings: Substitution<O, G, H>;
  readonly result: D;
}

/** For each g, argument(g) must lawfully represent the SAME subject as semantic(g). */
export interface RepresentedSubstitution<O, G extends GenericId, H extends GenericId, K extends Coordinates> {
  readonly semantic: Substitution<O, G, H>;
  readonly argument: (id: G) => Representation<Shape<O, H>, K>;
}

/**
 * Substitution keeps K, but changes the subject from S to its instantiated D.
 * This is not a subject-preserving, one-axis Transformation.
 * The application witness must match the input and the bindings.
 */
export interface RepresentationSubstitution<O, K extends Coordinates> {
  <G extends GenericId, H extends GenericId, S extends Shape<O, G>, D extends Shape<O, H>>(
    input: Representation<S, K>,
    bindings: RepresentedSubstitution<O, G, H, K>,
    application: SubstitutionApplication<O, G, H, S, D>,
  ): Representation<D, K>;
}

/**
 * A family over both open and closed shapes, commuting with substitution:
 * F(substitute_K(r, args)) ≈ substitute_L(F(r), g => F(args(g))).
 * Identity/associativity of substitution and congruence are also required.
 * As with NavigationTransparency, storing this record does not prove its laws.
 */
export interface SubstitutionTransparency<O, From extends Coordinates, To extends Coordinates> {
  readonly transform: ShapeRepresentationMap<O, From, To, GenericId>;
  readonly sourceSubstitute: RepresentationSubstitution<O, From>;
  readonly targetSubstitute: RepresentationSubstitution<O, To>;
  readonly equivalent: RepresentationEquivalence<To>;
}

// Implementation semantics. S is now SHAPE; X above is an arbitrary subject.
// @see ./foundations.md#3-native-realizations-instances-and-satisfaction
// The tree Shape<O, G> is one choice of S, not a mandatory encoding of every
// native type. A behavior domain fixes environments, effects and observations.

/** A reified mathematical proposition, not a Boolean test or evidence of truth. */
export interface Proposition {
  readonly statement: string;
  readonly parameters: Readonly<Record<string, unknown>>;
}

/** `equivalent` specifies an equivalence relation on behaviors at shape S. */
export interface BehaviorDomain<S, Beh> {
  readonly shape: S;
  readonly equivalent: (left: Beh, right: Beh) => Proposition;
}

/** B : Behavior[S] -> Prop. No decision procedure is required in general. */
export type BehaviorSpecification<Beh> = (behavior: Beh) => Proposition;

/**
 * C = (S, B), relative to a selected behavior domain. S = domain.shape.
 * Required law: B respects domain.equivalent. A shape-only contract uses B = true.
 */
export interface Contract<S, Beh> {
  readonly domain: BehaviorDomain<S, Beh>;
  readonly specification: BehaviorSpecification<Beh>;
}

/**
 * A selected native realization T in L, including its interpretation of values.
 * behaviorOf includes the correspondence to S, environment and initial/current
 * state as needed; it is not an algorithm inferred from a native type signature.
 * Values of T are structural candidates, not automatically lawful under B.
 */
export interface ModelType<S, Beh, L extends string, V> {
  readonly id: string;
  readonly language: L;
  readonly domain: BehaviorDomain<S, Beh>;
  readonly behaviorOf: (value: V) => Beh;
}

/** Erased constraint for indexing a particular selected ModelType without any. */
export interface ModelTypeIndex {
  readonly id: string;
  readonly language: string;
  readonly domain: { readonly shape: unknown };
  readonly behaviorOf: (value: never) => unknown;
}
export type NativeValue<T extends ModelTypeIndex> = Parameters<T['behaviorOf']>[0];
/** The carrier Behavior[S], not a particular behavior b in that carrier. */
export type ActualBehavior<T extends ModelTypeIndex> = ReturnType<T['behaviorOf']>;
export type ShapeOf<T extends ModelTypeIndex> = T['domain']['shape'];
export type ContractFor<T extends ModelTypeIndex> = Contract<ShapeOf<T>, ActualBehavior<T>>;

export interface ModelInstance<T extends ModelTypeIndex> {
  readonly modelType: T;
  readonly value: NativeValue<T>;
}

/** Intended witness: contract.specification(behavior) holds. Data is not proof. */
export interface Satisfaction<S, Beh, C extends Contract<S, Beh> = Contract<S, Beh>> {
  readonly contract: C;
  readonly behavior: Beh;
  readonly evidence: unknown;
}

/**
 * A lawful implementation of C. The witness must concern behaviorOf(value), or
 * an observationally equivalent behavior in the same domain (by B1). TypeScript
 * cannot establish satisfaction, exact domain identity, or that correspondence.
 * This is a reification of the refinement, not a trusted proof constructor.
 */
export interface ContractImplementation<T extends ModelTypeIndex, C extends ContractFor<T> = ContractFor<T>> {
  readonly instance: ModelInstance<T>;
  readonly satisfaction: Satisfaction<ShapeOf<T>, ActualBehavior<T>, C>;
}

/**
 * A candidate adapter between two realizations of a common shape/behavior domain.
 * Exact domain agreement remains a semantic obligation. The signature alone
 * establishes neither satisfaction preservation nor behavioral transparency.
 */
export type AdapterSemantics<T extends ModelTypeIndex, U extends ModelTypeIndex> =
  Equal<ShapeOf<T>, ShapeOf<U>> extends true
    ? Equal<ActualBehavior<T>, ActualBehavior<U>> extends true
      ? {
          readonly sourceType: T;
          readonly targetType: U;
          readonly adapt: (instance: ModelInstance<T>) => ModelInstance<U>;
        }
      : never
    : never;

/**
 * Syntax denotes its subject X; the artifact can be text OR a host AST value.
 * Mathematical Syntax[X, L] uses the same subject-first parameter order.
 * @see ./reference.md#objects-and-denotation
 */
export type Syntax<X, L extends string> = Representation<X, { readonly form: 'syntax'; readonly language: L }>;
export type ModelContractSyntax<S, Beh, D extends string> = Syntax<Contract<S, Beh>, D>;
export type ModelTypeSyntax<T extends ModelTypeIndex> = Syntax<T, T['language']>;
/** Denotes an initialized instance/configuration. Factory syntax has a different subject. */
export type ImplementationSyntax<T extends ModelTypeIndex> = Syntax<ModelInstance<T>, T['language']>;
export type AdapterSyntax<T extends ModelTypeIndex, U extends ModelTypeIndex, L extends string> = Syntax<
  AdapterSemantics<T, U>,
  L
>;

/**
 * Keep C alongside the generated type: native type syntax alone denotes T and
 * does not establish B for every native value. T realizes C's shape in its domain.
 */
export interface TypeGeneration<T extends ModelTypeIndex, C extends ContractFor<T> = ContractFor<T>> {
  readonly contract: C;
  readonly modelType: ModelTypeSyntax<T>;
}

/** A component with C and the chosen T fixed; foundations gives the full Pi/Sigma family. */
export type TypeGeneratorSemantics<C extends ContractFor<T>, T extends ModelTypeIndex, D extends string> = (
  input: Syntax<C, D>,
) => TypeGeneration<T, C>;

export interface AdapterGeneration<
  T extends ModelTypeIndex,
  U extends ModelTypeIndex,
  L extends T['language'],
  C extends ContractFor<T> = ContractFor<T>,
> extends TypeGeneration<T, C> {
  readonly adapter: AdapterSyntax<T, U, L>;
}

/**
 * The supplied T is the SAME T targeted by generated adapter semantics. For a
 * transparent generator, every admitted output adapter must preserve behavior;
 * retaining C and T in this record is necessary bookkeeping, not that proof.
 * This family emits adapters in T's native language; D and the host H are separate.
 */
export type AdapterGeneratorSemantics<
  C extends ContractFor<T>,
  T extends ModelTypeIndex,
  U extends ModelTypeIndex,
  D extends string,
  L extends T['language'],
> = (input: {
  readonly contract: Syntax<C, D>;
  readonly modelType: ModelTypeSyntax<T>;
}) => AdapterGeneration<T, U, L, C>;

/** Loaded generators are semantic functions; their source uses a separate host H. */
export type GeneratorSyntax<G, H extends string> = Syntax<G, H>;
