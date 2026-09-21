import { canonicalDeclaration, hashDeclaration, typeDeclaration } from './declaration_identity.ts';
import { callableBindingMetadata } from './validate.ts';
import type { TypeBinding } from './validate.ts';
import type { DeclarationIdentity } from './identity.ts';

type Node = { [key: string]: unknown };

function object(value: unknown): Node {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error('declaration: expected closed argument expression');
  return value as Node;
}

/** Identify a closed nominal callable from its canonical declaration and bound
 * arguments. An ordinary nongeneric callable keeps its declaring family digest. */
export function callableIdentity(binding: TypeBinding): DeclarationIdentity {
  const resolved = callableBindingMetadata(binding);
  const document = typeDeclaration(binding);
  const graph = object(JSON.parse(document)),
    root = object(graph.root);
  const applied = typeof root.apply === 'string',
    nominal = applied ? root.apply : root.ref;
  if (typeof nominal !== 'string') throw new Error('declaration: expected nominal callable root');
  const definition = object(object(graph.definitions)[nominal]);
  if (definition.kind !== 'callable') throw new Error('declaration: expected callable declaration');
  return { path: expressionName(root), digest: applied ? hashDeclaration(document) : resolved.digest };
}

function expressionName(value: unknown): string {
  const node = object(value);
  if (node.graph) return expressionName(object(node.graph).root);
  if (typeof node.primitive === 'string') return node.primitive;
  if (typeof node.ref === 'string') return node.ref;
  if (typeof node.apply === 'string') {
    if (!Array.isArray(node.arguments)) throw new Error('declaration: expected closed application arguments');
    return node.apply + '<' + node.arguments.map(expressionName).join(',') + '>';
  }
  for (const [key, prefix, suffix] of [
    ['array', '[', ']'],
    ['map', '{', '}'],
    ['nullable', '', '?'],
    ['entity', '&', ''],
  ] as const)
    if (key in node) return prefix + expressionName(node[key]) + suffix;
  if (typeof node.projection === 'string') return expressionName(node.family) + '/' + node.projection;
  if (typeof node.literal === 'string') return canonicalDeclaration(node.literal);
  if (node.empty === true) return '()';
  if (typeof node.kind === 'string') return 'shape(' + canonicalDeclaration(shapeName(node)) + ')';
  throw new Error('declaration: expected closed argument expression');
}

// Anonymous shapes have structural expression names; referenced definitions
// retain nominal paths here and contribute their revision content to the digest.
function shapeName(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(shapeName);
  if (!value || typeof value !== 'object') return value;
  const node = object(value);
  if (node.graph) return shapeName(object(node.graph).root);
  if (node.parameter || node.draw || node.unresolved)
    throw new Error('declaration: expected closed argument expression');
  return Object.fromEntries(Object.entries(node).map(([key, child]) => [key, shapeName(child)]));
}
