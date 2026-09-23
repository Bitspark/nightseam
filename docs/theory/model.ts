/**
 * A metamodel, expressed entirely as types. S and the meanings of the axes
 * are supplied by its users; there are no built-in languages or generator roles.
 *
 * Representation<S, K> means a representation OF S, not merely one tagged S.
 * Preserving S (including any chosen behavioral laws) is a semantic obligation.
 * TypeScript checks the indices and adjacency, not the meaning of an artifact.
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

export interface Representation<S, K extends Coordinates> {
  readonly subject: S;
  readonly coordinates: K;
  readonly artifact: unknown;
}

// An unrestricted map can interpret a path that changes zero or several axes.
export type RepresentationMap<S, From extends Coordinates, To extends Coordinates> = (
  input: Representation<S, From>,
) => Representation<S, To>;

type Equal<A, B> = [A] extends [B] ? ([B] extends [A] ? true : false) : false;
type IsUnion<T, Whole = T> = T extends Whole ? ([Whole] extends [T] ? false : true) : never;
type At<K, Axis extends PropertyKey> = Axis extends keyof K ? K[Axis] : never;

// Broad or union-valued coordinates describe families, not individual points.
type IsLiteral<T> = [T] extends [never]
  ? false
  : [T] extends [CoordinateValue]
    ? string extends T
      ? false
      : number extends T
        ? false
        : boolean extends T
          ? false
          : true extends IsUnion<T>
            ? false
            : true
    : false;

type IsPoint<K extends Coordinates> =
  true extends IsUnion<K>
    ? false
    : [keyof K] extends [AxisId]
      ? string extends keyof K
        ? false
        : false extends { [Axis in keyof K]-?: IsLiteral<K[Axis]> }[keyof K]
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
 * One elementary function, with the same S at both ends and exactly one axis
 * changed. This type is empty (`never`) when the endpoints are not adjacent.
 * Every inhabitant is required to preserve S; the signature alone is no proof
 * of shape isomorphism or behavioral preservation.
 */
export type Transformation<S, From extends Coordinates, To extends Coordinates> = [ChangedAxis<From, To>] extends [
  never,
]
  ? never
  : RepresentationMap<S, From, To>;

// The first-class signature: for fixed S, its identity is the ordered endpoint
// tuple. It determines the transformation TYPE, without selecting an inhabitant.
export type TransformationSignature<S, From extends Coordinates, To extends Coordinates> = [
  ChangedAxis<From, To>,
] extends [never]
  ? never
  : {
      readonly subject: S;
      readonly endpoints: readonly [from: From, to: To];
    };

// Several named functions may inhabit the same signature. `apply` is the
// function itself; its id is independent of the signature's endpoint identity.
export interface TransformationInstance<S, From extends Coordinates, To extends Coordinates> {
  readonly id: string;
  readonly signature: TransformationSignature<S, From, To>;
  readonly apply: Transformation<S, From, To>;
}

// A path lists all visited coordinates. One stop and zero steps is identity.
export type Stops = readonly [Coordinates, ...Coordinates[]];

export type PathSteps<S, Route extends Stops> = Route extends readonly [
  infer From extends Coordinates,
  infer To extends Coordinates,
  ...infer Rest extends Coordinates[],
]
  ? readonly [Transformation<S, From, To>, ...PathSteps<S, readonly [To, ...Rest]>]
  : readonly [];

export interface Path<S, Route extends Stops> {
  readonly subject: S;
  // A finite tuple makes every link checkable. An unbounded array loses that evidence.
  readonly coordinates: number extends Route['length'] ? never : Route;
  readonly steps: PathSteps<S, Route>;
}

type Last<Route extends Stops> = Route extends readonly [...Coordinates[], infer K extends Coordinates] ? K : never;

// Interpreting a path composes its steps; it need not be an elementary step.
export type PathMeaning<S, Route extends Stops> = RepresentationMap<S, Route[0], Last<Route>>;

// Contract structure specializes the previously opaque subject S. Contract keys
// are independent of coordinate-axis IDs, even though both use opaque strings.
export type ContractKey = string;
export type ContractPath = readonly ContractKey[];

export type GenericId = string;

/** A hole stands for an ENTIRE contract; it has no local value or children. */
export type Generic<G extends GenericId> = [G] extends [never] ? never : { readonly kind: 'generic'; readonly id: G };

/** An immutable, finite, acyclic node. Children have unique keys; order is immaterial. */
export interface ContractNode<O, G extends GenericId = never> {
  readonly kind: 'node';
  readonly value?: O;
  readonly children: ReadonlyMap<ContractKey, Contract<O, G>>;
}

/** G is the allowed free-hole context. G = never recovers closed contracts. */
export type Contract<O = unknown, G extends GenericId = never> = ContractNode<O, G> | Generic<G>;

// An existing node without a value or children is still Found, not Missing.
export type Selection<T> = { readonly kind: 'found'; readonly value: T } | { readonly kind: 'missing' };

/** Required laws: at(C, []) = C; at(C, p ++ q) = bind(at(C, p), at(_, q)). */
export type ContractNavigation<O, G extends GenericId = never> = (
  contract: Contract<O, G>,
  path: ContractPath,
) => Selection<Contract<O, G>>;

/**
 * A location witnesses that selected = at(root, path). Construction must establish
 * this equation. TypeScript relates the subject types but cannot prove the lookup.
 */
export interface ContractLocation<
  O,
  Root extends Contract<O, G>,
  G extends GenericId = never,
  Selected extends Contract<O, G> = Contract<O, G>,
> {
  readonly root: Root;
  readonly path: ContractPath;
  readonly selected: Selected;
}

/**
 * Given a valid location, select the corresponding representation at the SAME
 * coordinate K. Navigation may retain the context needed to interpret a fragment.
 * Its result's subject is the selected sub-contract, not the original root.
 */
export interface RepresentationNavigation<O, K extends Coordinates, G extends GenericId = never> {
  <Root extends Contract<O, G>, Selected extends Contract<O, G>>(
    input: Representation<Root, K>,
    location: ContractLocation<O, Root, G, Selected>,
  ): Representation<Selected, K>;
}

/** The observable contract surface at ONE node. Child order is not semantic. */
export interface ContractNodeSurface<O> {
  readonly kind: 'node';
  readonly value?: O;
  readonly keys: ReadonlySet<ContractKey>;
}

export type ContractSurface<O, G extends GenericId = never> = ContractNodeSurface<O> | Generic<G>;

/**
 * Observe the represented artifact, not just its subject field. Required law:
 * observe(at_K(r, p)) = surface(at(r.subject, p)) for every valid path p.
 * Equality of opaque values is supplied by the model's user.
 */
export interface RepresentationObservation<O, K extends Coordinates, G extends GenericId = never> {
  <C extends Contract<O, G>>(input: Representation<C, K>): ContractSurface<O, G>;
}

/** An equivalence relation, respected by navigation and admitted transformations. */
export interface RepresentationEquivalence<K extends Coordinates> {
  <S>(left: Representation<S, K>, right: Representation<S, K>): boolean;
}

// A polymorphic map supplies a component for EVERY contract in the chosen domain.
// The domain here is finite Contract<O, G> trees, and is closed under navigation.
export interface ContractRepresentationMap<
  O,
  From extends Coordinates,
  To extends Coordinates,
  G extends GenericId = never,
> {
  <C extends Contract<O, G>>(input: Representation<C, From>): Representation<C, To>;
}

export type TransformationFamily<O, From extends Coordinates, To extends Coordinates, G extends GenericId = never> = [
  ChangedAxis<From, To>,
] extends [never]
  ? never
  : ContractRepresentationMap<O, From, To, G>;

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

export type FamilyPathMeaning<O, Route extends Stops, G extends GenericId = never> = ContractRepresentationMap<
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
  readonly transform: ContractRepresentationMap<O, From, To, G>;
  readonly sourceAt: RepresentationNavigation<O, From, G>;
  readonly targetAt: RepresentationNavigation<O, To, G>;
  readonly equivalent: RepresentationEquivalence<To>;
}

/** A total, simultaneous assignment of source holes to contracts over target holes. */
export type Substitution<O, G extends GenericId, H extends GenericId> = (id: G) => Contract<O, H>;

/** Substitute once: a replacement is returned as-is, not traversed under the same assignment. */
export interface ContractSubstitution<O> {
  <G extends GenericId, H extends GenericId>(template: Contract<O, G>, bindings: Substitution<O, G, H>): Contract<O, H>;
}

/** A semantic witness: result = template[bindings], preserving the particular result subject. */
export interface SubstitutionApplication<
  O,
  G extends GenericId,
  H extends GenericId,
  C extends Contract<O, G>,
  D extends Contract<O, H>,
> {
  readonly template: C;
  readonly bindings: Substitution<O, G, H>;
  readonly result: D;
}

/** For each g, argument(g) must lawfully represent the SAME subject as semantic(g). */
export interface RepresentedSubstitution<O, G extends GenericId, H extends GenericId, K extends Coordinates> {
  readonly semantic: Substitution<O, G, H>;
  readonly argument: (id: G) => Representation<Contract<O, H>, K>;
}

/**
 * Substitution keeps K, but changes the subject from C to its instantiated D.
 * This is not a subject-preserving, one-axis Transformation.
 * The application witness must match the input and the bindings.
 */
export interface RepresentationSubstitution<O, K extends Coordinates> {
  <G extends GenericId, H extends GenericId, C extends Contract<O, G>, D extends Contract<O, H>>(
    input: Representation<C, K>,
    bindings: RepresentedSubstitution<O, G, H, K>,
    application: SubstitutionApplication<O, G, H, C, D>,
  ): Representation<D, K>;
}

/**
 * A family over both open and closed contracts, commuting with substitution:
 * F(substitute_K(r, args)) ≈ substitute_L(F(r), g => F(args(g))).
 * Identity/associativity of substitution and congruence are also required.
 * As with NavigationTransparency, storing this record does not prove its laws.
 */
export interface SubstitutionTransparency<O, From extends Coordinates, To extends Coordinates> {
  readonly transform: ContractRepresentationMap<O, From, To, GenericId>;
  readonly sourceSubstitute: RepresentationSubstitution<O, From>;
  readonly targetSubstitute: RepresentationSubstitution<O, To>;
  readonly equivalent: RepresentationEquivalence<To>;
}
