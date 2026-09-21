// The runtime interpreter reads the declaration's own expressions from a
// family descriptor. Imports and type arguments retain the family where
// they were declared, including through nested generic applications.
// Go and TypeScript share the acceptance and diagnostic cases in
// conformance/tables/validator.json. TypeScript's runtime slots supply the
// bindings that generated Go codecs also carry in their instantiated types.
import { scalarValue } from './unicode.ts';

/** An expression as the declaration writes it, including unnamed shapes. */
export type TypeExpression =
  | string
  | { array: TypeExpression }
  | { map: TypeExpression }
  | { nullable: TypeExpression }
  | { literal: string }
  | { ref: string }
  | { apply: string; with: Record<string, TypeExpression> }
  | { empty: true }
  | WireType;

export interface WireParameter {
  name: string;
  of?: string;
}

/** One record member, with presence independent of nullness. */
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

/** A declaration's own fields and variants, before inheritance is expanded. */
export interface WireType {
  kind: string;
  key?: string;
  fields?: WireField[];
  extends?: (string | { apply: string; with: Record<string, TypeExpression> })[];
  open?: boolean;
  values?: string[];
  type?: TypeExpression;
  parameters?: WireParameter[];
  tag?: string;
  value?: string;
  variants?: Record<string, TypeExpression>;
  contract?: string;
  request?: TypeExpression;
  result?: TypeExpression;
}

/** A family's declarations and the parameters in their enclosing scope. */
export interface WireFamily {
  types: Record<string, WireType>;
  parameters?: WireParameter[];
}

export interface AnyFamily {
  readonly name: string;
  Envelope: unknown;
  Handle: unknown;
}

/** A family bound to a parameter, retaining its descriptor for nested applications. */
export interface FamilyBinding<F extends AnyFamily> {
  readonly name: F['name'];
  readonly validate: Validator;
}

/** A type argument interpreted in the family whose validator is supplied. */
export interface TypeBinding {
  readonly type: TypeExpression;
  readonly validate: Validator;
  readonly slots?: Slots;
}

export type Slots = { readonly [parameter: string]: FamilyBinding<AnyFamily> | TypeBinding };

const descriptor = Symbol('validator descriptor');
interface Schema {
  family: WireFamily;
  imported: Record<string, Validator>;
}
type Scope = Record<string, { type: Expression } | { family: Schema }>;
interface Expression {
  schema: Schema;
  value: TypeExpression;
  scope: Scope;
  aliases?: ReadonlySet<WireType>;
}
interface Resolved extends Expression {
  definition?: WireType;
  name?: string;
}

/** A callable validator carrying the declaration context needed by its importers. */
export interface Validator {
  (type: TypeExpression, value: unknown, location?: string, slots?: Slots): void;
  readonly [descriptor]: Schema;
}
function timestamp(value: unknown): boolean {
  if (typeof value !== 'string') return false;
  const m = /^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(?:\.\d+)?(?:Z|([+-])(\d\d):(\d\d))$/.exec(value);
  if (!m) return false;
  const year = Number(m[1]),
    month = Number(m[2]),
    day = Number(m[3]);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return (
    month >= 1 &&
    month <= 12 &&
    day >= 1 &&
    day <= days[month - 1]! &&
    Number(m[4]) <= 23 &&
    Number(m[5]) <= 59 &&
    Number(m[6]) <= 59 &&
    (!m[7] || (Number(m[8]) <= 23 && Number(m[9]) <= 59)) &&
    !Number.isNaN(Date.parse(value))
  );
}

function jsonValue(value: unknown, location: string, seen = new Set<object>()): void {
  if (value === null || typeof value === 'string' || typeof value === 'boolean') return;
  if (typeof value === 'number') {
    if (!Number.isFinite(value)) throw new Error(location + ': expected finite JSON number');
    return;
  }
  if (typeof value !== 'object' || seen.has(value)) throw new Error(location + ': expected acyclic JSON');
  seen.add(value);
  if (Array.isArray(value)) {
    let index = 0;
    for (const child of value) jsonValue(child, location + '[' + index++ + ']', seen);
  } else {
    if (Object.getPrototypeOf(value) !== Object.prototype && Object.getPrototypeOf(value) !== null)
      throw new Error(location + ': expected plain JSON object');
    for (const key of Object.keys(value).sort())
      jsonValue((value as Record<string, unknown>)[key], location + '.' + key, seen);
  }
  seen.delete(value);
}

function plainObject(value: unknown): value is Record<string, unknown> {
  return (
    value !== null &&
    typeof value === 'object' &&
    !Array.isArray(value) &&
    (Object.getPrototypeOf(value) === Object.prototype || Object.getPrototypeOf(value) === null)
  );
}

function diagnosticLiteral(value: string): string {
  return JSON.stringify(value).replace(
    /[<>&\u2028\u2029]/g,
    (character) => '\\u' + character.charCodeAt(0).toString(16).padStart(4, '0'),
  );
}

// These syntax exclusions are the declaration's existing dialect rules.
// Matching semantics are held separately by value cases, not by compilation.
const outsidePattern = [
  '(?P<',
  '(?P=',
  '(?<=',
  '(?<!',
  '(?<',
  '(?=',
  '(?!',
  '[[:',
  '\\k<',
  '\\p{',
  '\\P{',
  '\\A',
  '\\z',
  '\\Z',
  '\\Q',
  '\\C',
];

function checkPattern(value: string): void {
  const refuse = (): never => {
    throw new Error('pattern ' + diagnosticLiteral(value) + ': outside Nightseam dialect');
  };
  let inClass = false;
  for (let index = 0; index < value.length; index++) {
    const rest = value.slice(index);
    if (value[index] === '\\') {
      if (outsidePattern.some((prefix) => rest.startsWith(prefix))) refuse();
      if (/^\\[1-9]/.test(rest)) refuse();
      index++;
    } else if (!inClass) {
      if (outsidePattern.some((prefix) => rest.startsWith(prefix))) refuse();
      if (/^\(\?[imsUu-]+[):]/.test(rest)) refuse();
      // Compare decimal bounds exactly; engines may saturate large counts
      // before checking their order, admitting a reversed interval.
      const bounds = /^\{([0-9]+),([0-9]+)\}/.exec(rest);
      if (bounds) {
        const lower = bounds[1]!.replace(/^0+/, '') || '0';
        const upper = bounds[2]!.replace(/^0+/, '') || '0';
        if (lower.length > upper.length || (lower.length === upper.length && lower > upper)) refuse();
      }
      if (value[index] === '[') inClass = true;
    } else if (value[index] === ']') inClass = false;
  }
  try {
    new RegExp(value, 'u');
  } catch {
    refuse();
  }
}

function checkPatterns(value: unknown, seen = new Set<object>()): void {
  if (!value || typeof value !== 'object' || seen.has(value)) return;
  seen.add(value);
  if (Array.isArray(value)) {
    for (const item of value) checkPatterns(item, seen);
  } else {
    const object = value as Record<string, unknown>;
    if (typeof object.pattern === 'string') checkPattern(object.pattern);
    for (const key of Object.keys(object).sort()) checkPatterns(object[key], seen);
  }
}

/** Holds a field's value to its constraints: min and max on a number or a timestamp, length on a string or an array, pattern on a string. */
function constrain(field: WireField, value: unknown, location: string): void {
  if (field.min !== undefined || field.max !== undefined) {
    if (typeof value === 'number') {
      if (field.min !== undefined && value < field.min) throw new Error(location + ': expected at least ' + field.min);
      if (field.max !== undefined && value > field.max) throw new Error(location + ': expected at most ' + field.max);
    } else if (typeof value === 'string') {
      if (field.min !== undefined && value < String(field.min))
        throw new Error(location + ': expected at or after ' + field.min);
      if (field.max !== undefined && value > String(field.max))
        throw new Error(location + ': expected at or before ' + field.max);
    }
  }
  if (field.length) {
    const length = typeof value === 'string' ? [...value].length : Array.isArray(value) ? value.length : -1;
    if (length >= 0) {
      if (field.length.min !== undefined && length < field.length.min)
        throw new Error(location + ': expected a length of at least ' + field.length.min);
      if (field.length.max !== undefined && length > field.length.max)
        throw new Error(location + ': expected a length of at most ' + field.length.max);
    }
  }
  if (field.pattern && typeof value === 'string' && !new RegExp(field.pattern, 'u').test(value)) {
    throw new Error(location + ': expected a match of ' + field.pattern);
  }
}

function bad(location: string, want: string): never {
  throw new Error(location + ': expected ' + want);
}

function child(expression: Expression, value: TypeExpression): Expression {
  return { schema: expression.schema, scope: expression.scope, value };
}

function singleFamilyParameter(schema: Schema): WireParameter | undefined {
  const parameters = schema.family.parameters?.filter((parameter) => parameter.of);
  return parameters?.length === 1 ? parameters[0] : undefined;
}

function freeParameters(schema: Schema, type: WireType | undefined, seen = new Set<WireType>()): WireParameter[] {
  if (!type || seen.has(type)) return [];
  seen.add(type);
  const used = new Set<string>();
  const inherit = (type: WireType | undefined): void => {
    for (const parameter of freeParameters(schema, type, seen)) used.add(parameter.name);
  };
  const walkBase = (base: TypeExpression): void => {
    if (typeof base === 'object' && base !== null && 'apply' in base) {
      for (const filler of Object.values(base.with)) walk(filler);
    } else walk(base);
  };
  const walk = (value: TypeExpression | undefined): void => {
    if (typeof value === 'string') {
      const dot = value.indexOf('.'),
        prefix = dot < 0 ? value : value.slice(0, dot);
      if (schema.family.parameters?.some((parameter) => parameter.name === prefix)) {
        used.add(prefix);
        return;
      }
      if (dot < 0) {
        inherit(schema.family.types[value]);
        return;
      }
      const imported = schema.imported[prefix]?.[descriptor];
      const target = imported?.family.types[value.slice(dot + 1)];
      if (imported && target) {
        const needed = [...freeParameters(imported, target, seen), ...(target.parameters ?? [])];
        const source = singleFamilyParameter(schema);
        if (source && needed.some((parameter) => parameter.of)) used.add(source.name);
      }
    } else if (value && typeof value === 'object') {
      if ('array' in value) walk(value.array);
      else if ('map' in value) walk(value.map);
      else if ('nullable' in value) walk(value.nullable);
      else if ('apply' in value) {
        if (!value.apply.includes('.')) inherit(schema.family.types[value.apply]);
        for (const filler of Object.values(value.with)) walk(filler);
      } else if ('ref' in value) {
        const entity = schema.family.types[value.ref];
        walk(entity?.fields?.find((field) => field.name === entity.key)?.type);
      } else if ('kind' in value) {
        for (const field of value.fields ?? []) walk(field.type);
        for (const base of value.extends ?? []) walkBase(base);
        for (const variant of Object.values(value.variants ?? {})) walk(variant);
      }
    }
  };
  walk(type.type);
  for (const field of type.fields ?? []) walk(field.type);
  for (const base of type.extends ?? []) walkBase(base);
  for (const variant of Object.values(type.variants ?? {})) walk(variant);
  seen.delete(type);
  return (schema.family.parameters ?? []).filter((parameter) => used.has(parameter.name));
}

function named(expression: Expression, name: string, location: string): [Expression, WireType, string] {
  const dot = name.indexOf('.');
  if (dot >= 0) {
    const caller = expression;
    const family = name.slice(0, dot);
    let schema: Schema | undefined;
    if (/^[A-Z]/.test(family)) {
      const argument = expression.scope[family];
      schema = argument && 'family' in argument ? argument.family : undefined;
      if (!schema) bad(location, 'a binding of the parameter ' + family);
    } else {
      schema = expression.schema.imported[family]?.[descriptor];
      if (!schema) bad(location, 'known family');
    }
    name = name.slice(dot + 1);
    expression = { schema, value: name, scope: {} };
    if (/^[a-z]/.test(family)) {
      const source = singleFamilyParameter(caller.schema),
        target = schema.family.types[name];
      if (source && target) {
        const binding = caller.scope[source.name];
        if (binding) {
          for (const parameter of [...freeParameters(schema, target), ...(target.parameters ?? [])])
            if (parameter.of) expression.scope[parameter.name] = binding;
        }
      }
    }
  }
  if (!Object.hasOwn(expression.schema.family.types, name)) bad(location, 'known type');
  return [child(expression, name), expression.schema.family.types[name]!, name];
}

function resolve(expression: Expression, location: string, inheritance = false): Resolved {
  let aliases = new Set(expression.aliases);
  for (;;) {
    let definition: WireType, name: string;
    const value = expression.value;
    if (typeof value === 'string') {
      const argument = Object.hasOwn(expression.scope, value) ? expression.scope[value] : undefined;
      if (argument) {
        if (!('type' in argument)) bad(location, 'type argument');
        expression = argument.type;
        aliases = new Set(expression.aliases);
        continue;
      }
      if (['json', 'string', 'boolean', 'number', 'integer', 'timestamp'].includes(value)) return expression;
      [expression, definition, name] = named(expression, value, location);
    } else if (value !== null && typeof value === 'object') {
      if ('apply' in value) {
        const [target, type, targetName] = named(expression, value.apply, location);
        if (!plainObject(value.with)) bad(location, 'application arguments');
        const scope: Scope = { ...target.scope };
        const parameters = [
          ...(inheritance || value.apply.includes('.') ? freeParameters(target.schema, type) : []),
          ...(type.parameters ?? []),
        ];
        const allowed = new Set<string>();
        for (const parameter of parameters) {
          allowed.add(parameter.name);
          if (!Object.hasOwn(value.with, parameter.name)) bad(location, 'an argument for ' + parameter.name);
          const filler = value.with[parameter.name]!;
          if (!parameter.of) {
            // Restore the argument's ancestry on substitution: Id<Id<T>>
            // terminates, while A<T> = Id<A<T>> expands A a second time.
            scope[parameter.name] = { type: { ...child(expression, filler), aliases: new Set(aliases) } };
          } else {
            if (typeof filler !== 'string') bad(location, 'family argument for ' + parameter.name);
            const argument = expression.scope[filler];
            const family =
              argument && 'family' in argument ? argument.family : expression.schema.imported[filler]?.[descriptor];
            if (!family) bad(location, 'known family argument for ' + parameter.name);
            scope[parameter.name] = { family };
          }
        }
        for (const parameter of Object.keys(value.with).sort())
          if (!allowed.has(parameter)) bad(location, 'known parameter ' + parameter);
        expression = { ...target, scope };
        definition = type;
        name = targetName;
      } else if ('kind' in value) {
        definition = value;
        name = value.kind;
      } else return expression;
    } else return bad(location, 'type expression');
    inheritance = false;
    if (definition.kind === 'alias') {
      if (aliases.has(definition)) bad(location, 'acyclic type expression');
      aliases.add(definition);
      expression = child(expression, definition.type!);
      continue;
    }
    return { ...expression, definition, name };
  }
}

interface ScopedField {
  field: WireField;
  expression: Expression;
}

function inherited(expression: Expression, base: TypeExpression, location: string): Resolved {
  if (typeof base === 'string') {
    const [owner, definition] = named(expression, base, location);
    if (definition.parameters?.length || freeParameters(owner.schema, definition).length)
      bad(location, 'explicit application of generic base ' + base);
  }
  return resolve(child(expression, base), location, true);
}
function fields(expression: Resolved, location: string, seen = new Set<WireType>()): ScopedField[] {
  const definition = expression.definition;
  if (!definition) bad(location, 'record');
  if (seen.has(definition)) bad(location, 'acyclic inheritance');
  seen.add(definition);
  const result = (definition.extends ?? []).flatMap((base) =>
    fields(inherited(expression, base, location), location, seen),
  );
  for (const field of definition.fields ?? []) result.push({ field, expression: child(expression, field.type) });
  seen.delete(definition);
  return result;
}

function variants(expression: Resolved, location: string, seen = new Set<WireType>()): Record<string, Expression> {
  const definition = expression.definition;
  if (!definition || definition.kind !== 'union') bad(location, 'union');
  if (seen.has(definition)) bad(location, 'acyclic inheritance');
  seen.add(definition);
  const result: Record<string, Expression> = Object.create(null) as Record<string, Expression>;
  for (const base of definition.extends ?? [])
    Object.assign(result, variants(inherited(expression, base, location), location, seen));
  for (const [tag, variant] of Object.entries(definition.variants ?? {})) result[tag] = child(expression, variant);
  seen.delete(definition);
  return result;
}

function nullable(expression: Expression, location: string): boolean {
  const resolved = resolve(expression, location);
  return typeof resolved.value === 'object' && resolved.value !== null && 'nullable' in resolved.value;
}

function validate(expression: Expression, value: unknown, location: string): void {
  const resolved = resolve(expression, location);
  const definition = resolved.definition;
  if (definition) {
    switch (definition.kind) {
      case 'callable': {
        // A live value on the wire is a reference to one binding: the
        // binding, opaque here, and the contract it implements. The contract
        // is nominal, so the only reference this position accepts is one
        // declared as this callable. Validation resolves nothing, registers
        // nothing and reaches no network; whether the binding exists, is
        // still alive or belongs to this scope is the live runtime's to
        // answer when it imports it.
        if (!plainObject(value)) bad(location, 'a live reference to ' + definition.contract);
        const reference = value as Record<string, unknown>;
        if (typeof reference['binding'] !== 'string' || reference['binding'] === '') {
          throw new Error(location + '.binding: a live reference names the binding it refers to');
        }
        if (typeof reference['contract'] !== 'string') {
          throw new Error(location + '.contract: a live reference carries the declaration it implements');
        }
        if (reference['contract'] !== definition.contract) {
          throw new Error(
            location +
              '.contract: the reference carries ' +
              reference['contract'] +
              ' where ' +
              definition.contract +
              ' is expected',
          );
        }
        for (const key of Object.keys(reference).sort()) {
          if (key !== 'binding' && key !== 'contract') throw new Error(location + '.' + key + ': unknown field');
        }
        return;
      }
      case 'enum':
        if (typeof value !== 'string' || !definition.values?.includes(value)) bad(location, resolved.name!);
        return;
      case 'record':
      case 'entity': {
        if (!plainObject(value)) bad(location, resolved.name + ' object');
        const object = value as Record<string, unknown>;
        const allowed = new Set<string>();
        for (const scoped of fields(resolved, location)) {
          const { field } = scoped;
          allowed.add(field.name);
          const at = location + '.' + field.name;
          if (!Object.hasOwn(object, field.name)) {
            if (field.required) throw new Error(at + ': required field missing');
            continue;
          }
          const member = object[field.name];
          if (member === null) {
            if (field.nullable) continue;
            if (!nullable(scoped.expression, at)) throw new Error(at + ': null is not permitted');
          }
          validate(scoped.expression, member, at);
          if (member !== null) constrain(field, member, at);
        }
        for (const key of Object.keys(object).sort()) {
          if (allowed.has(key)) continue;
          if (!definition.open) throw new Error(location + '.' + key + ': unknown field');
          jsonValue(object[key], location + '.' + key);
        }
        return;
      }
      case 'union': {
        if (!plainObject(value)) bad(location, resolved.name + ' object');
        const object = value as Record<string, unknown>,
          tag = definition.tag!;
        if (!Object.hasOwn(object, tag)) throw new Error(location + '.' + tag + ': required field missing');
        const tagName = object[tag],
          all = variants(resolved, location);
        if (typeof tagName !== 'string' || !Object.hasOwn(all, tagName)) bad(location + '.' + tag, 'known variant');
        const variant = all[tagName]!,
          member = definition.value ?? 'value';
        const marker = variant.value;
        const empty =
          typeof marker === 'object' &&
          marker !== null &&
          'empty' in marker &&
          marker.empty === true &&
          Object.keys(marker).length === 1;
        if (!empty) {
          if (!Object.hasOwn(object, member)) throw new Error(location + '.' + member + ': required field missing');
          validate(variant, object[member], location + '.' + member);
        }
        for (const key of Object.keys(object).sort())
          if (key !== tag && (empty || key !== member)) throw new Error(location + '.' + key + ': unknown field');
        return;
      }
      default:
        bad(location, 'supported type');
    }
  }
  const type = resolved.value;
  if (typeof type === 'object') {
    if ('nullable' in type) {
      if (value !== null) validate(child(resolved, type.nullable), value, location);
      return;
    }
    if ('literal' in type) {
      if (typeof type.literal !== 'string' || !type.literal) bad(location, 'nonempty string literal');
      const literal = diagnosticLiteral(type.literal);
      if (value !== type.literal) bad(location, 'literal ' + literal);
      return;
    }
    if ('array' in type) {
      if (!Array.isArray(value)) bad(location, 'array');
      for (let index = 0; index < value.length; index++)
        validate(child(resolved, type.array), value[index], location + '[' + index + ']');
      return;
    }
    if ('map' in type) {
      if (!plainObject(value)) bad(location, 'object');
      const object = value as Record<string, unknown>;
      for (const key of Object.keys(object).sort())
        validate(child(resolved, type.map), object[key], location + '.' + key);
      return;
    }
    if ('ref' in type) {
      let target: Expression, entity: WireType;
      try {
        [target, entity] = named(resolved, type.ref, location);
      } catch {
        return bad(location, 'known entity');
      }
      const key = fields(resolve(target, location), location).find(({ field }) => field.name === entity.key);
      if (!key) bad(location, 'entity with a key');
      validate(key.expression, value, location);
      return;
    }
    if ('empty' in type) {
      if (!plainObject(value) || Object.keys(value as object).length !== 0) bad(location, 'empty object');
      return;
    }
    bad(location, 'supported type expression');
  }
  switch (type) {
    case 'json':
      jsonValue(value, location);
      return;
    case 'string':
      if (typeof value !== 'string') bad(location, 'string');
      return;
    case 'boolean':
      if (typeof value !== 'boolean') bad(location, 'boolean');
      return;
    case 'number':
    case 'integer':
      if (typeof value !== 'number') bad(location, type);
      if (!Number.isFinite(value)) bad(location, 'finite number');
      if (type === 'integer' && !Number.isSafeInteger(value)) bad(location, 'JavaScript-safe integer');
      return;
    case 'timestamp':
      if (typeof value !== 'string') bad(location, 'timestamp');
      if (!timestamp(value)) bad(location, 'RFC3339 timestamp');
      return;
    default:
      bad(location, 'known type');
  }
}

/** Creates a validator whose imports retain both values and declaration scope. */
function boundScope(slots: Slots, active = new Set<Slots>()): Scope {
  if (active.has(slots)) throw new Error('cyclic type argument bindings');
  active.add(slots);
  try {
    const scope: Scope = Object.create(null) as Scope;
    for (const [parameter, binding] of Object.entries(slots)) {
      scalarValue(parameter);
      if ('type' in binding) {
        scalarValue(binding.type);
        checkPatterns(binding.type);
        scope[parameter] = {
          type: {
            schema: binding.validate[descriptor],
            value: binding.type,
            scope: boundScope(binding.slots ?? {}, active),
          },
        };
      } else {
        scope[parameter] = { family: binding.validate[descriptor] };
      }
    }
    return scope;
  } finally {
    active.delete(slots);
  }
}

export function createValidator(family: WireFamily, imported: Record<string, Validator> = {}): Validator {
  scalarValue(family);
  scalarValue(Object.keys(imported));
  if (!family.types) throw new Error('expected family descriptor with types');
  checkPatterns(family.types);
  const schema: Schema = { family, imported };
  const validateWire = (type: TypeExpression, value: unknown, location = '$', slots: Slots = {}): void => {
    scalarValue(type);
    scalarValue(value);
    checkPatterns(type);
    const scope = boundScope(slots);
    validate({ schema, value: type, scope }, value, location);
  };
  return Object.assign(validateWire, { [descriptor]: schema });
}
