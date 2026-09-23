/**
 * Finite, deterministic, total BooleanCell semantics. TypeScript models native
 * realizations; it does not execute Go or infer behavior from arbitrary code.
 * Product exploration checks all finite interaction traces of each given pair,
 * not merely traces up to a chosen length. No concurrency, failure or divergence
 * is admitted by this example's observation model.
 * @see ../reference.md#objects-and-denotation — B1, I1, A1, and A2.
 * @see ../foundations.md#2-shape-specification-and-actual-behavior — Behavior and specification
 * @see ../foundations.md#3-native-realizations-instances-and-satisfaction — Satisfaction
 * @see ../foundations.md#5-adapters-generators-and-behavioral-preservation — Adapters and generators
 */
import assert from 'node:assert/strict';
import { Script } from 'node:vm';
import type {
  AdapterGeneratorSemantics,
  AdapterSemantics,
  BehaviorDomain,
  Contract,
  ContractImplementation,
  GeneratorSyntax,
  ModelInstance,
  ModelType,
  ModelTypeSyntax,
  Satisfaction,
  Syntax,
  TypeGeneratorSemantics,
} from '../model.ts';
import { booleanCell as cellShape } from './coordinates.ts';

export type CellInput = { readonly kind: 'read' } | { readonly kind: 'write'; readonly value: boolean };
export type CellOutput = { readonly kind: 'read'; readonly value: boolean } | { readonly kind: 'written' };
export interface CellMachine {
  readonly states: readonly string[];
  readonly initial: string;
  readonly step: (state: string, input: CellInput) => { readonly state: string; readonly output: CellOutput };
}
export const inputs: readonly CellInput[] = [
  { kind: 'read' },
  { kind: 'write', value: false },
  { kind: 'write', value: true },
];

// Check closure in the finite state/output domain. Purity and determinism of
// step are assumptions; one traversal cannot establish them for arbitrary JS.
export function validate(machine: CellMachine): void {
  assert.equal(new Set(machine.states).size, machine.states.length);
  assert.ok(machine.states.includes(machine.initial));
  for (const state of machine.states) {
    for (const input of inputs) {
      const next = machine.step(state, input);
      assert.ok(machine.states.includes(next.state));
      assert.equal(next.output.kind, input.kind === 'read' ? 'read' : 'written');
      if (next.output.kind === 'read') assert.equal(typeof next.output.value, 'boolean');
    }
  }
}

const sameOutput = (left: CellOutput, right: CellOutput): boolean =>
  left.kind === 'written' ? right.kind === 'written' : right.kind === 'read' && left.value === right.value;

/** A shortest distinguishing trace, or null when every finite trace agrees. */
export function distinguish(left: CellMachine, right: CellMachine): readonly CellInput[] | null {
  validate(left);
  validate(right);
  const pending = [{ left: left.initial, right: right.initial, trace: [] as readonly CellInput[] }];
  const visited = new Set<string>();
  for (let cursor = 0; cursor < pending.length; cursor++) {
    const pair = pending[cursor];
    const key = JSON.stringify([pair.left, pair.right]);
    if (visited.has(key)) continue;
    visited.add(key);
    for (const input of inputs) {
      const a = left.step(pair.left, input);
      const b = right.step(pair.right, input);
      const trace = [...pair.trace, input];
      if (!sameOutput(a.output, b.output)) return trace;
      pending.push({ left: a.state, right: b.state, trace });
    }
  }
  return null;
}
export const equivalent = (left: CellMachine, right: CellMachine): boolean => distinguish(left, right) === null;

export const referenceCell = (initial: boolean): CellMachine => ({
  states: ['false', 'true'],
  initial: String(initial),
  step: (state, input) =>
    input.kind === 'read'
      ? { state, output: { kind: 'read', value: state === 'true' } }
      : { state: String(input.value), output: { kind: 'written' } },
});

export const cellDomain: BehaviorDomain<typeof cellShape, CellMachine> = {
  shape: cellShape,
  // A proposition describes a semantic claim; it is neither a Boolean decision
  // procedure nor a proof. `equivalent` above decides this particular finite case.
  equivalent: (left, right) => ({
    statement: 'Every finite read/write trace has the same outputs.',
    parameters: { left, right },
  }),
};
export const cellContract = {
  domain: cellDomain,
  specification: (behavior: CellMachine) => ({
    statement: 'Observationally equivalent to a storing cell, with either initial Boolean value.',
    parameters: { behavior },
  }),
} satisfies Contract<typeof cellShape, CellMachine>;

export function satisfiesCell(behavior: CellMachine): boolean {
  return equivalent(behavior, referenceCell(false)) || equivalent(behavior, referenceCell(true));
}

// State-explicit meanings of two possible Go realizations. These are semantic
// fixtures for a method interface and a function record, not their Go source.
export interface MethodValue {
  readonly initial: boolean;
  readonly Read: (state: boolean) => { readonly state: boolean; readonly value: boolean };
  readonly Write: (state: boolean, value: boolean) => boolean;
}
export interface FunctionValue {
  readonly initial: 0 | 1;
  readonly get: (state: 0 | 1) => { readonly state: 0 | 1; readonly value: boolean };
  readonly set: (state: 0 | 1, value: boolean) => 0 | 1;
}
export const methodType = {
  id: 'go:method-cell',
  language: 'go',
  domain: cellDomain,
  behaviorOf: (value: MethodValue): CellMachine => ({
    states: ['false', 'true'],
    initial: String(value.initial),
    step: (state, input) => {
      if (input.kind === 'write') {
        return { state: String(value.Write(state === 'true', input.value)), output: { kind: 'written' } };
      }
      const result = value.Read(state === 'true');
      return { state: String(result.state), output: { kind: 'read', value: result.value } };
    },
  }),
} as const satisfies ModelType<typeof cellShape, CellMachine, 'go', MethodValue>;
export const functionType = {
  id: 'go:function-cell',
  language: 'go',
  domain: cellDomain,
  behaviorOf: (value: FunctionValue): CellMachine => ({
    states: ['0', '1'],
    initial: String(value.initial),
    step: (state, input) => {
      if (input.kind === 'write') {
        return { state: String(value.set(state === '1' ? 1 : 0, input.value)), output: { kind: 'written' } };
      }
      const result = value.get(state === '1' ? 1 : 0);
      return { state: String(result.state), output: { kind: 'read', value: result.value } };
    },
  }),
} as const satisfies ModelType<typeof cellShape, CellMachine, 'go', FunctionValue>;

export const methodCell = (initial: boolean, ignoresWrites = false): ModelInstance<typeof methodType> => ({
  modelType: methodType,
  value: {
    initial,
    Read: (state) => ({ state, value: state }),
    Write: (state, value) => (ignoresWrites ? state : value),
  },
});
export const functionCell: ModelInstance<typeof functionType> = {
  modelType: functionType,
  value: { initial: 1, get: (state) => ({ state, value: state === 1 }), set: (_state, value) => (value ? 1 : 0) },
};
export const falseCell = methodCell(false);
export const trueCell = methodCell(true);
export const ignoresWrites = methodCell(false, true);
export const behaviorOf = (instance: ModelInstance<typeof methodType>): CellMachine =>
  instance.modelType.behaviorOf(instance.value);

function establish(behavior: CellMachine): Satisfaction<typeof cellShape, CellMachine, typeof cellContract> {
  assert.ok(satisfiesCell(behavior), 'The instance must satisfy the cell specification.');
  return {
    contract: cellContract,
    behavior,
    evidence: { method: 'Exhaustive finite product exploration against the two reference initial states.' },
  };
}
export const lawfulCell: ContractImplementation<typeof methodType, typeof cellContract> = {
  instance: trueCell,
  satisfaction: establish(behaviorOf(trueCell)),
};

function checkImplementation(implementation: ContractImplementation<typeof methodType, typeof cellContract>): void {
  assert.equal(implementation.satisfaction.contract, cellContract);
  assert.ok(
    equivalent(behaviorOf(implementation.instance), implementation.satisfaction.behavior),
    'Witness must concern this instance.',
  );
  assert.ok(satisfiesCell(implementation.satisfaction.behavior));
}

// A semantic wire surface for this example, with no transport failures. This is
// an interpretation of the cell's interactions, not Nightseam's actual Wire API.
export interface SurfaceValue {
  readonly machine: CellMachine;
}
export const surfaceType = {
  id: 'cell:interaction-surface',
  language: 'interaction-model',
  domain: cellDomain,
  behaviorOf: (value: SurfaceValue): CellMachine => value.machine,
} as const satisfies ModelType<typeof cellShape, CellMachine, 'interaction-model', SurfaceValue>;
export const forwardingAdapter = {
  sourceType: methodType,
  targetType: surfaceType,
  adapt: (instance: ModelInstance<typeof methodType>): ModelInstance<typeof surfaceType> => ({
    modelType: surfaceType,
    value: { machine: behaviorOf(instance) },
  }),
} satisfies AdapterSemantics<typeof methodType, typeof surfaceType>;
export const replacingAdapter = {
  ...forwardingAdapter,
  adapt: (_instance: ModelInstance<typeof methodType>): ModelInstance<typeof surfaceType> => ({
    modelType: surfaceType,
    value: { machine: referenceCell(false) },
  }),
} satisfies AdapterSemantics<typeof methodType, typeof surfaceType>;
const adaptedBehavior = (
  adapter: AdapterSemantics<typeof methodType, typeof surfaceType>,
  instance: ModelInstance<typeof methodType>,
) => {
  const result = adapter.adapt(instance);
  return result.modelType.behaviorOf(result.value);
};

// A loaded generator is a semantic function on syntax values. The declaration
// and native-type fixtures stipulate denotation; no Go parser/compiler is used.
// Adapter output is a contextual Go identifier expression. Its interpretation
// binds BindBooleanCell to forwardingAdapter; it references an available adapter,
// rather than supplying its implementation as a comment-only source placeholder.
export const contractSyntax: Syntax<typeof cellContract, 'cell-declaration'> = {
  subject: cellContract,
  coordinates: { form: 'syntax', language: 'cell-declaration' },
  artifact: 'BooleanCell { read: Unit -> Bool; write: Bool -> Unit; laws: storing-cell }',
};
export const methodSyntax: ModelTypeSyntax<typeof methodType> = {
  subject: methodType,
  coordinates: { form: 'syntax', language: 'go' },
  artifact: 'type BooleanCell interface { Read() bool; Write(value bool) }',
};
export const generateType: TypeGeneratorSemantics<typeof cellContract, typeof methodType, 'cell-declaration'> = (
  input,
) => ({
  contract: input.subject,
  modelType: methodSyntax,
});
export const generateAdapter: AdapterGeneratorSemantics<
  typeof cellContract,
  typeof methodType,
  typeof surfaceType,
  'cell-declaration',
  'go'
> = (input) => {
  assert.equal(input.modelType.subject, methodType);
  return {
    contract: input.contract.subject,
    modelType: input.modelType,
    adapter: {
      subject: forwardingAdapter,
      coordinates: { form: 'syntax', language: 'go' },
      artifact: 'BindBooleanCell',
    },
  };
};
export const typeGeneration = generateType(contractSyntax);
export const adapterGeneration = generateAdapter({ contract: contractSyntax, modelType: typeGeneration.modelType });
// Node executes the stripped TypeScript as JavaScript. This actual function
// expression denotes g in the explicitly supplied host environment below.
export const generatorSyntax: GeneratorSyntax<typeof generateAdapter, 'javascript'> = {
  subject: generateAdapter,
  coordinates: { form: 'syntax', language: 'javascript' },
  artifact: generateAdapter.toString(),
};
const adapterBindings = new Map([['BindBooleanCell', forwardingAdapter]]);
assert.equal(typeof adapterGeneration.adapter.artifact, 'string');
assert.equal(adapterBindings.get(adapterGeneration.adapter.artifact as string), adapterGeneration.adapter.subject);
assert.equal(typeof generatorSyntax.artifact, 'string');
const loadedGenerator: typeof generateAdapter = new Script(`(${generatorSyntax.artifact})`).runInNewContext({
  assert,
  methodType,
  forwardingAdapter,
});
const loadedResult = loadedGenerator({ contract: contractSyntax, modelType: methodSyntax });
assert.equal(loadedResult.contract, cellContract);
assert.equal(loadedResult.modelType, methodSyntax);
assert.equal(loadedResult.adapter.subject, forwardingAdapter);
assert.equal(loadedResult.adapter.artifact, adapterGeneration.adapter.artifact);

assert.ok(satisfiesCell(behaviorOf(falseCell)));
assert.ok(satisfiesCell(behaviorOf(trueCell)));
assert.equal(equivalent(behaviorOf(falseCell), behaviorOf(trueCell)), false);
assert.ok(equivalent(behaviorOf(trueCell), functionType.behaviorOf(functionCell.value)));
assert.equal(satisfiesCell(behaviorOf(ignoresWrites)), false);
assert.throws(() => establish(behaviorOf(ignoresWrites)), /must satisfy/);
checkImplementation(lawfulCell);
assert.throws(() => checkImplementation({ ...lawfulCell, instance: ignoresWrites }), /this instance/);
assert.deepEqual(distinguish(behaviorOf(ignoresWrites), referenceCell(false)), [inputs[2], inputs[0]]);
assert.ok(equivalent(adaptedBehavior(forwardingAdapter, trueCell), behaviorOf(trueCell)));
assert.ok(satisfiesCell(adaptedBehavior(replacingAdapter, trueCell)));
assert.deepEqual(distinguish(adaptedBehavior(replacingAdapter, trueCell), behaviorOf(trueCell)), [inputs[0]]);
assert.equal(adapterGeneration.contract, cellContract);
assert.equal(adapterGeneration.modelType.subject, typeGeneration.modelType.subject);
assert.equal(adapterGeneration.adapter.subject.sourceType, methodType);
assert.equal(generatorSyntax.subject, generateAdapter);

// Every deterministic two-state method machine: 2 initial states, four possible
// read values, four read-transition tables, and sixteen write-transition tables.
// Equivalent behaviors satisfy B alike; forwarding preserves even unlawful
// behavior, while replacement guarantees only a lawful result.
export let checkedMachines = 0;
for (const initial of [false, true]) {
  for (let reads = 0; reads < 4; reads++) {
    for (let readNext = 0; readNext < 4; readNext++) {
      for (let writeNext = 0; writeNext < 16; writeNext++) {
        const bit = (table: number, index: number) => Boolean(table & (1 << index));
        const instance: ModelInstance<typeof methodType> = {
          modelType: methodType,
          value: {
            initial,
            Read: (state) => ({ state: bit(readNext, Number(state)), value: bit(reads, Number(state)) }),
            Write: (state, value) => bit(writeNext, 2 * Number(state) + Number(value)),
          },
        };
        const original = behaviorOf(instance);
        const forwarded = adaptedBehavior(forwardingAdapter, instance);
        assert.ok(equivalent(original, forwarded));
        assert.equal(satisfiesCell(original), satisfiesCell(forwarded));
        assert.ok(satisfiesCell(adaptedBehavior(replacingAdapter, instance)));
        checkedMachines++;
      }
    }
  }
}
assert.equal(checkedMachines, 512);
assert.throws(() => validate({ ...referenceCell(false), initial: 'missing' }));
assert.throws(() =>
  validate({ ...referenceCell(false), step: () => ({ state: 'missing', output: { kind: 'written' } }) }),
);

// Renderer data comes from the checked semantics, not separately authored claims.
export const cases = [
  { name: 'Storing cell · false', machine: behaviorOf(falseCell) },
  { name: 'Storing cell · true', machine: behaviorOf(trueCell) },
  { name: 'Ignores writes', machine: behaviorOf(ignoresWrites) },
  { name: 'Forwarded true cell', machine: adaptedBehavior(forwardingAdapter, trueCell) },
  { name: 'Replaced with fresh false cell', machine: adaptedBehavior(replacingAdapter, trueCell) },
].map(({ name, machine }) => ({
  name,
  lawful: satisfiesCell(machine),
  sameAsTrueCell: equivalent(machine, behaviorOf(trueCell)),
  initial: machine.initial,
  transitions: machine.states.map((state) => ({ state, steps: inputs.map((input) => machine.step(state, input)) })),
}));
console.log({
  behaviorMachines: checkedMachines,
  cases: cases.map(({ name, lawful, sameAsTrueCell }) => ({ name, lawful, sameAsTrueCell })),
});
