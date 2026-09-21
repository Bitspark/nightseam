import { scalarValue } from './unicode.ts';
import { createValidator, validatorMetadata } from './validate.ts';
import type { AnyFamily, FamilyBinding, Slots, TypeBinding, TypeExpression, Validator, WireType } from './validate.ts';

type ObjectValue = { [key: string]: Value };
type Value = string | number | boolean | null | Value[] | ObjectValue;
interface Graph extends ObjectValue {
  definitions: ObjectValue;
  root: Value;
  version: 1;
}
const declarations = new WeakMap<Validator, Graph>();

function object(value: Value | undefined): ObjectValue {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('declaration: expected object');
  return value;
}

/** The exact shared UTF-8 JSON spelling, including Go's HTML and line-separator escapes. */
export function canonicalDeclaration(value: unknown): string {
  scalarValue(value);
  const encode = (value: unknown): string => {
    if (typeof value === 'string')
      return JSON.stringify(value).replace(
        /[<>&\u2028\u2029]/g,
        (character) => '\\u' + character.charCodeAt(0).toString(16).padStart(4, '0'),
      );
    if (value === null || typeof value === 'boolean' || value === 1) return String(value);
    if (Array.isArray(value)) return '[' + value.map(encode).join(',') + ']';
    if (value && typeof value === 'object') {
      const item = value as Record<string, unknown>;
      // Scalar-code-point order equals UTF-8 order; JS's UTF-16 sort does not.
      return (
        '{' +
        Object.keys(item)
          .sort(compareUTF8)
          .map((key) => encode(key) + ':' + encode(item[key]))
          .join(',') +
        '}'
      );
    }
    throw new Error('declaration: numbers other than version 1 must be normalized strings');
  };
  return encode(value);
}

function compareUTF8(left: string, right: string): number {
  const a = [...left],
    b = [...right];
  for (let index = 0; index < Math.min(a.length, b.length); index++) {
    const difference = a[index]!.codePointAt(0)! - b[index]!.codePointAt(0)!;
    if (difference) return difference;
  }
  return a.length - b.length;
}

function readDeclaration(document: string): Graph {
  const value = object(JSON.parse(document) as Value);
  if (value.version !== 1 || !value.root) throw new Error('declaration: expected a version 1 declaration graph');
  object(value.definitions);
  if (canonicalDeclaration(value) !== document) throw new Error('declaration: expected canonical bytes');
  return value as Graph;
}

/** Attach generated provenance without changing the explicit digest or validation semantics. */
export function withDeclaration(validator: Validator, document: string): Validator {
  const graph = readDeclaration(document);
  const schema = validatorMetadata(validator);
  if (schema.digest && schema.digest !== hashDeclaration(document))
    throw new Error('declaration: digest does not identify its canonical graph');
  const attached = createValidator(schema.family, schema.digest, schema.imported);
  declarations.set(attached, graph);
  return attached;
}

/** Close a family's required parameters using the supplied declaration interpretations. */
export function boundDeclaration(validator: Validator, slots: Slots = {}): string {
  return canonicalDeclaration(new Builder().family(validator, slots));
}

/** The SHA-256 identity of a closed family interpretation. Missing required slots are errors. */
export function declarationDigest(validator: Validator, slots: Slots = {}): string {
  return hashDeclaration(boundDeclaration(validator, slots));
}

/** Select one argument's reachable declarations, retaining nested binding scopes. */
export function typeDeclaration(binding: TypeBinding): string {
  return canonicalDeclaration(new Builder().expression(binding, 0));
}

class Builder {
  private readonly active = new Set<Slots>();

  family(validator: Validator, slots: Slots): Graph {
    if (this.active.has(slots)) throw new Error('declaration: cyclic supplied bindings');
    this.active.add(slots);
    try {
      const graph = this.graph(validator),
        root = object(graph.root);
      if (typeof root.ref !== 'string' || object(graph.definitions[root.ref]).kind !== 'family')
        throw new Error('declaration: expected family root');
      return this.named(graph, root.ref, slots, 0);
    } finally {
      this.active.delete(slots);
    }
  }

  private graph(validator: Validator): Graph {
    const graph = declarations.get(validator);
    if (!graph) throw new Error('declaration: expected canonical declaration provenance');
    return graph;
  }

  private named(graph: Graph, path: string, slots: Slots, depth: number): Graph {
    const definition = object(graph.definitions[path]);
    const parameters = [...((definition.captures as Value[]) ?? []), ...((definition.parameters as Value[]) ?? [])];
    let root: Value = { ref: path };
    if (parameters.length) {
      const args: Value[] = parameters.map((item) => {
        const parameter = object(item),
          name = String(parameter.name),
          binding = slots[name];
        if (!binding) throw new Error('declaration: missing required binding ' + name);
        if (parameter.of !== '') {
          if ('type' in binding) throw new Error('declaration: expected family binding ' + name);
          return { graph: this.family(binding.validate, binding.slots ?? {}) };
        }
        if (!('type' in binding)) throw new Error('declaration: expected type binding ' + name);
        return { graph: this.expression(binding, depth + 1) };
      });
      root = { apply: path, arguments: args };
    }
    return select(graph, root);
  }

  expression(binding: TypeBinding, depth: number): Graph {
    if (depth > 256) throw new Error('declaration: cyclic supplied bindings');
    const { validate, type } = binding,
      slots = binding.slots ?? {};
    const simple = (root: Value): Graph => ({ version: 1, root, definitions: {} });
    if (typeof type === 'string') {
      const supplied = slots[type];
      if (supplied) {
        if (!('type' in supplied)) throw new Error('declaration: expected type binding ' + type);
        return this.expression(supplied, depth + 1);
      }
      if (['string', 'integer', 'number', 'boolean', 'json', 'timestamp'].includes(type))
        return simple({ primitive: type });
      const target = this.lookup(type, validate, slots);
      if (target.definition.kind === 'alias') {
        if (!target.definition.type) throw new Error('declaration: expected alias target');
        return this.expression(
          { type: target.definition.type, validate: target.validate, slots: target.slots },
          depth + 1,
        );
      }
      const graph = this.graph(target.validate),
        family = object(graph.root).ref;
      return this.named(graph, family + '/' + target.name, target.slots, depth);
    }
    for (const key of ['array', 'map', 'nullable'] as const) {
      if (key in type) {
        const graph = this.expression(
          { type: (type as Record<string, TypeExpression>)[key]!, validate, slots },
          depth + 1,
        );
        graph.root = { [key]: graph.root };
        return graph;
      }
    }
    if ('literal' in type) return simple({ literal: type.literal });
    if ('empty' in type && type.empty) return simple({ empty: true });
    if ('ref' in type) {
      const graph = this.expression({ type: type.ref, validate, slots }, depth + 1);
      graph.root = { entity: graph.root };
      return graph;
    }
    if ('apply' in type) {
      const target = this.lookup(type.apply, validate, slots);
      const bound: Record<string, FamilyBinding<AnyFamily> | TypeBinding> = { ...target.slots };
      const graph = this.graph(target.validate),
        family = object(graph.root).ref;
      const definition = graph.definitions[family + '/' + target.name]
        ? object(graph.definitions[family + '/' + target.name])
        : {};
      const parameters = [...((definition.captures as Value[]) ?? []), ...((definition.parameters as Value[]) ?? [])];
      // Alias templates may have no nominal definition after normalization.
      const expected = new Map<string, string>();
      for (const parameter of parameters) {
        const p = object(parameter);
        expected.set(String(p.name), String(p.of));
      }
      for (const parameter of target.definition.parameters ?? []) expected.set(parameter.name, parameter.of ?? '');
      for (const [name, value] of Object.entries(type.with)) {
        const of = expected.get(name);
        if (of === undefined) throw new Error('declaration: unknown parameter ' + name);
        if (of) {
          if (typeof value !== 'string') throw new Error('declaration: expected family binding ' + name);
          const source = slots[value];
          if (source && !('type' in source)) bound[name] = source;
          else {
            const imported = validatorMetadata(validate).imported[value];
            if (!imported) throw new Error('declaration: expected family binding ' + name);
            bound[name] = { name: value, validate: imported };
          }
        } else bound[name] = { type: value, validate, slots };
      }
      if (target.definition.kind === 'alias') {
        if (!target.definition.type) throw new Error('declaration: expected alias target');
        return this.expression({ type: target.definition.type, validate: target.validate, slots: bound }, depth + 1);
      }
      return this.named(graph, family + '/' + target.name, bound, depth);
    }
    throw new Error('declaration: type argument has no canonical declaration provenance');
  }

  private lookup(
    name: string,
    validator: Validator,
    slots: Slots,
  ): { name: string; validate: Validator; slots: Slots; definition: WireType } {
    const dot = name.indexOf('.');
    if (dot >= 0) {
      const prefix = name.slice(0, dot),
        binding = slots[prefix];
      if (binding && !('type' in binding)) {
        validator = binding.validate;
        slots = binding.slots ?? {};
      } else {
        const imported = validatorMetadata(validator).imported[prefix];
        if (!imported) throw new Error('declaration: expected known family ' + prefix);
        validator = imported;
        slots = {};
      }
      name = name.slice(dot + 1);
    }
    const definition = validatorMetadata(validator).family.types[name];
    if (!definition) throw new Error('declaration: expected known type ' + name);
    return { name, validate: validator, slots, definition };
  }
}

function select(source: Graph, root: Value): Graph {
  const selected: Graph = { version: 1, root, definitions: {} };
  const include = (path: string): void => {
    if (Object.hasOwn(selected.definitions, path)) return;
    const definition = source.definitions[path];
    if (!definition) throw new Error('declaration: unknown reference ' + path);
    selected.definitions[path] = definition;
    visit(definition);
  };
  const visit = (value: Value): void => {
    if (Array.isArray(value)) {
      value.forEach(visit);
      return;
    }
    if (!value || typeof value !== 'object' || 'graph' in value) return;
    for (const key of ['ref', 'apply']) if (typeof value[key] === 'string') include(value[key] as string);
    for (const [key, child] of Object.entries(value)) if (key !== 'ref' && key !== 'apply') visit(child);
  };
  visit(root);
  return selected;
}

// FIPS 180-4 SHA-256, using only browser arithmetic and TextEncoder. The shared
// canonical fixtures and standard vectors hold its bytes independently of Go.
export function hashDeclaration(document: string): string {
  const input = new TextEncoder().encode(document);
  const size = Math.ceil((input.length + 9) / 64) * 64;
  const data = new Uint8Array(size);
  data.set(input);
  data[input.length] = 0x80;
  const view = new DataView(data.buffer);
  view.setUint32(size - 8, Math.floor(input.length / 0x20000000));
  view.setUint32(size - 4, (input.length * 8) >>> 0);
  const state = [0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19];
  const words = new Uint32Array(64),
    rotate = (v: number, n: number): number => (v >>> n) | (v << (32 - n));
  for (let block = 0; block < size; block += 64) {
    for (let i = 0; i < 16; i++) words[i] = view.getUint32(block + i * 4);
    for (let i = 16; i < 64; i++) {
      const a = words[i - 15]!,
        b = words[i - 2]!;
      words[i] =
        (words[i - 16]! +
          (rotate(a, 7) ^ rotate(a, 18) ^ (a >>> 3)) +
          words[i - 7]! +
          (rotate(b, 17) ^ rotate(b, 19) ^ (b >>> 10))) >>>
        0;
    }
    let [a, b, c, d, e, f, g, h] = state as [number, number, number, number, number, number, number, number];
    for (let i = 0; i < 64; i++) {
      const t1 =
        (h + (rotate(e, 6) ^ rotate(e, 11) ^ rotate(e, 25)) + ((e & f) ^ (~e & g)) + constants[i]! + words[i]!) >>> 0;
      const t2 = ((rotate(a, 2) ^ rotate(a, 13) ^ rotate(a, 22)) + ((a & b) ^ (a & c) ^ (b & c))) >>> 0;
      h = g;
      g = f;
      f = e;
      e = (d + t1) >>> 0;
      d = c;
      c = b;
      b = a;
      a = (t1 + t2) >>> 0;
    }
    const result = [a, b, c, d, e, f, g, h];
    for (let i = 0; i < 8; i++) state[i] = (state[i]! + result[i]!) >>> 0;
  }
  return state.map((value) => value.toString(16).padStart(8, '0')).join('');
}

const constants = [
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5, 0xd807aa98,
  0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786,
  0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da, 0x983e5152, 0xa831c66d, 0xb00327c8,
  0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
  0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819,
  0xd6990624, 0xf40e3585, 0x106aa070, 0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a,
  0x5b9cca4f, 0x682e6ff3, 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7,
  0xc67178f2,
];
