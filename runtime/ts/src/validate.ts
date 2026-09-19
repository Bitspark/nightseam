// The wire validator every generated client carries: a family's types as
// the generator describes them — a record's fields with their presence,
// nullness and constraints, its parents, an enum's values, an alias's
// target — validated against JSON values. A generated client embeds its
// family's description and makes one validator of it; a type of another
// family is validated by that family's validator, handed over by name; a
// type drawn from a parameter by the binding of the family that fills it,
// passed in at the call, since that family is chosen where the client is
// instantiated. (The Go runtime, whose generated codecs validate what fills
// a slot where the generic type is instantiated, passes such a value
// through instead.)
//
// A type expression is read as the contract writes it: a primitive, a type
// of the family, "family.Type" for an imported family's, "S.Type" for a
// parameter's, {array: T}, {map: T}, {ref: "Entity"} for the entity's key,
// {apply: "family.Type", with: {...}} for an imported generic type, and
// {empty: true} for a request that takes nothing.
//
// A refusal is one string: a JSON pointer naming the member that was wrong,
// then the fact about it — "$.count: required field missing", "$.note: null
// is not permitted", "$.zzz: unknown field". The Go runtime prints the same
// string for the same value, word for word, and
// conformance/tables/validator.json holds both to it, so a consumer whose
// server is in one language and client in the other reads one spelling of
// one refusal.

/** A type as the embedded descriptor spells it: a primitive or named type, an array, a map, a reference, or an application of a generic type. */
export type TypeExpression =
  | string
  | { array: TypeExpression }
  | { map: TypeExpression }
  | { ref: string }
  | { apply: string; with: Record<string, string> }
  | { empty: true };

/** One member of a record in the descriptor: its wire name, its type, whether it must be present, and the constraints the validator holds it to. */
export interface WireField {
  name: string;
  type: TypeExpression;
  required?: boolean;
  nullable?: boolean;
  unique?: boolean;
  min?: number;
  max?: number;
  length?: { min?: number; max?: number };
  pattern?: string;
}

/** One type of the descriptor — a record, entity, enum or alias — as the validator reads it. */
export interface WireType {
  kind: string;
  key?: string;
  fields?: WireField[];
  extends?: string[];
  open?: boolean;
  values?: string[];
  type?: TypeExpression;
}

/** What a slot of the session role is filled with: any family. */
export interface AnyFamily { readonly name: string; Envelope: unknown; Handle: unknown }

/** A family bound at runtime: its name and its validator, which validates what fills a slot of it. */
export interface FamilyBinding<F extends AnyFamily> {
  readonly name: F['name'];
  validate(type: TypeExpression, value: unknown, location?: string): void;
}

/** The families bound to the parameters a value's slots name. */
export type Slots = { readonly [parameter: string]: FamilyBinding<AnyFamily> };

/** A validator: of a type expression's value, at a location, with the bindings of the parameters. */
export type Validator = (type: TypeExpression, value: unknown, location?: string, slots?: Slots) => void;

function timestamp(value: unknown): boolean {
  if (typeof value !== 'string') return false;
  const m = /^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(?:\.\d+)?(?:Z|([+-])(\d\d):(\d\d))$/.exec(value);
  if (!m) return false;
  const year = Number(m[1]), month = Number(m[2]), day = Number(m[3]);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return month >= 1 && month <= 12 && day >= 1 && day <= days[month - 1]! && Number(m[4]) <= 23 && Number(m[5]) <= 59 && Number(m[6]) <= 59 && (!m[7] || (Number(m[8]) <= 23 && Number(m[9]) <= 59)) && !Number.isNaN(Date.parse(value));
}

function jsonValue(value: unknown, location: string, seen = new Set<object>()): void {
  if (value === null || typeof value === 'string' || typeof value === 'boolean') return;
  if (typeof value === 'number') {
    if (!Number.isFinite(value)) throw new Error(location + ': expected finite JSON number');
    return;
  }
  if (typeof value !== 'object' || seen.has(value)) throw new Error(location + ': expected acyclic JSON');
  seen.add(value);
  if (Array.isArray(value)) { let index = 0; for (const child of value) jsonValue(child, location + '[' + (index++) + ']', seen); }
  else {
    if (Object.getPrototypeOf(value) !== Object.prototype && Object.getPrototypeOf(value) !== null) throw new Error(location + ': expected plain JSON object');
    for (const [key, child] of Object.entries(value)) jsonValue(child, location + '.' + key, seen);
  }
  seen.delete(value);
}

function plainObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value) && (Object.getPrototypeOf(value) === Object.prototype || Object.getPrototypeOf(value) === null);
}

/** Holds a field's value to its constraints: min and max on a number or a timestamp, length on a string or an array, pattern on a string. */
function constrain(field: WireField, value: unknown, location: string): void {
  if (field.min !== undefined || field.max !== undefined) {
    if (typeof value === 'number') {
      if (field.min !== undefined && value < field.min) throw new Error(location + ': expected at least ' + field.min);
      if (field.max !== undefined && value > field.max) throw new Error(location + ': expected at most ' + field.max);
    } else if (typeof value === 'string') {
      if (field.min !== undefined && value < String(field.min)) throw new Error(location + ': expected at or after ' + field.min);
      if (field.max !== undefined && value > String(field.max)) throw new Error(location + ': expected at or before ' + field.max);
    }
  }
  if (field.length) {
    const length = typeof value === 'string' ? [...value].length : Array.isArray(value) ? value.length : -1;
    if (length >= 0) {
      if (field.length.min !== undefined && length < field.length.min) throw new Error(location + ': expected a length of at least ' + field.length.min);
      if (field.length.max !== undefined && length > field.length.max) throw new Error(location + ': expected a length of at most ' + field.length.max);
    }
  }
  if (field.pattern && typeof value === 'string' && !new RegExp(field.pattern).test(value)) {
    throw new Error(location + ': expected a match of ' + field.pattern);
  }
}

/**
 * Makes a family's validator of its wire description and the validators of
 * the families it refers to. Runtime validation applies equally to calls,
 * replies, reverse calls and events.
 */
export function createValidator(types: Record<string, WireType>, imported: Record<string, Validator> = {}): Validator {
  const fields = (name: string): WireField[] => {
    const type = types[name]!;
    return [...(type.extends ?? []).flatMap(fields), ...(type.fields ?? [])];
  };
  const foreign = (reference: string, value: unknown, location: string, slots?: Slots): void => {
    const at = reference.indexOf('.');
    const validate = imported[reference.slice(0, at)];
    if (!validate) throw new Error(location + ': expected known family');
    validate(reference.slice(at + 1), value, location, slots);
  };
  /** What fills a slot of a parameter is validated by the binding of the family that fills it; a client always passes its bindings, so a validation without one is a call that lost them. */
  const drawn = (parameter: string, type: string, value: unknown, location: string, slots?: Slots): void => {
    const binding = slots?.[parameter];
    if (!binding) throw new Error(location + ': expected a binding of the parameter ' + parameter);
    binding.validate(type, value, location);
  };
  /** An application binds the applied type's parameters as it says: a named family by its validator, a parameter of this family by the caller's binding of it. */
  const applied = (fillers: Record<string, string>, slots?: Slots): Slots => {
    const bound: Record<string, FamilyBinding<AnyFamily>> = {};
    for (const [parameter, filler] of Object.entries(fillers)) {
      if (/^[A-Z]/.test(filler)) { if (slots?.[filler]) bound[parameter] = slots[filler]; continue; }
      const validate = imported[filler];
      if (validate) bound[parameter] = { name: filler, validate: (type, value, location) => validate(type, value, location) };
    }
    return bound;
  };
  const validateWire: Validator = (type, value, location = '$', slots) => {
    const bad = (expected: string): never => { throw new Error(location + ': expected ' + expected); };
    if (typeof type === 'object') {
      if ('array' in type) {
        if (!Array.isArray(value)) bad('array');
        let index = 0;
        for (const item of value as unknown[]) validateWire(type.array, item, location + '[' + (index++) + ']', slots);
        return;
      }
      if ('ref' in type) {
        const entity = types[type.ref];
        if (!entity) bad('known entity');
        const key = fields(type.ref).find((field) => field.name === entity!.key);
        if (!key) bad('entity with a key');
        validateWire(key!.type, value, location, slots);
        return;
      }
      if ('apply' in type) { foreign(type.apply, value, location, applied(type.with, slots)); return; }
      if ('map' in type) {
        if (!plainObject(value)) bad('object');
        for (const [key, item] of Object.entries(value as Record<string, unknown>)) validateWire(type.map, item, location + '.' + key, slots);
        return;
      }
      if (!('empty' in type)) bad('supported type expression');
      if (!plainObject(value) || Object.keys(value as object).length !== 0) bad('empty object');
      return;
    }
    if (type.includes('.')) {
      const at = type.indexOf('.');
      if (/^[A-Z]/.test(type)) { drawn(type.slice(0, at), type.slice(at + 1), value, location, slots); return; }
      foreign(type, value, location, slots);
      return;
    }
    switch (type) {
      case 'json': jsonValue(value, location); return;
      case 'string': if (typeof value !== 'string') bad('string'); return;
      case 'boolean': if (typeof value !== 'boolean') bad('boolean'); return;
      case 'number': case 'integer':
        if (typeof value !== 'number') bad(type);
        if (!Number.isFinite(value)) bad('finite number');
        if (type === 'integer' && !Number.isSafeInteger(value)) bad('JavaScript-safe integer');
        return;
      case 'timestamp':
        if (typeof value !== 'string') bad('timestamp');
        if (!timestamp(value)) bad('RFC3339 timestamp');
        return;
    }
    const definition = types[type];
    if (!definition) bad('known type');
    if (definition!.kind === 'alias') { validateWire(definition!.type!, value, location, slots); return; }
    if (definition!.kind === 'enum') { if (typeof value !== 'string' || !definition!.values!.includes(value)) bad(type); return; }
    if (definition!.kind !== 'record' && definition!.kind !== 'entity') bad('supported type');
    if (!plainObject(value)) bad(type + ' object');
    const object = value as Record<string, unknown>, allowed = new Set<string>();
    for (const field of fields(type)) {
      allowed.add(field.name);
      const at = location + '.' + field.name;
      if (!Object.hasOwn(object, field.name)) {
        if (field.required === true) throw new Error(at + ': required field missing');
        continue;
      }
      const child = object[field.name];
      if (child === null && field.nullable) continue;
      if (child === null) throw new Error(at + ': null is not permitted');
      validateWire(field.type, child, at, slots);
      constrain(field, child, at);
    }
    for (const key of Object.keys(object)) {
      if (allowed.has(key)) continue;
      if (!definition!.open) throw new Error(location + '.' + key + ': unknown field');
      jsonValue(object[key], location + '.' + key);
    }
  };
  return validateWire;
}
