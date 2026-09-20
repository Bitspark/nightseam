/**
 * The higher-order half of the TypeScript generated testee: a callable whose
 * own request or result is another callable.
 *
 * This is the position nothing exercised before. A record of callables and a
 * callback retained past its call each go through one conversion; a callable
 * that *takes* or *answers* a callable goes through a conversion nested inside
 * a conversion, on the far side of a binding that was itself exported. The
 * generated code compiled long before it worked, which is why this is driven
 * rather than only built.
 *
 * TypeScript renders a client and no binding, so serving is unsupported here
 * and the runs that matter are the ones with a Go binding on the other side:
 * this peer's functions are handed to it and invoked there, and the ones it
 * answers are invoked here.
 */
import * as combinator from './api/ts/combinator-client/src/index.ts';

type Args = Record<string, unknown>;

export class CombinatorFailure extends Error {
  readonly code: string;
  constructor(code: string, message: string) {
    super(message);
    this.code = code;
  }
  toJSON(): Record<string, unknown> {
    return { code: this.code, message: this.message };
  }
}

class CombinatorDialled {
  client!: combinator.Client;
  private readonly toolkits = new Map<string, combinator.Toolkit>();
  private next = 0;

  hold(toolkit: combinator.Toolkit): string {
    this.next += 1;
    const name = 'toolkit' + String(this.next);
    this.toolkits.set(name, toolkit);
    return name;
  }

  toolkit(name: string): combinator.Toolkit {
    const toolkit = this.toolkits.get(name);
    if (!toolkit) throw new CombinatorFailure('invalid', 'no toolkit ' + name);
    return toolkit;
  }

  shutdown(): void {
    this.client.close();
  }
}

const handles = new Map<string, CombinatorDialled>();
let next = 0;

export function resetCombinator(): void {
  for (const d of handles.values()) d.shutdown();
  handles.clear();
}

function lookup(args: Args): CombinatorDialled {
  const d = handles.get(String(args.on));
  if (!d) throw new CombinatorFailure('unknown_handle', String(args.on));
  return d;
}

const unsupported = () => {
  throw new CombinatorFailure('unsupported', 'TypeScript renders a client and no binding');
};

export const combinatorOps: Record<string, (args: Args) => unknown | Promise<unknown>> = {
  'client.combinator_pack': async (args: Args) => {
    const add = Number(args.add);
    const batch = await lookup(args).client.pack({ item: async (value: number) => value + add });
    const some = batch.items[1];
    if (some?.kind !== 'some') throw new CombinatorFailure('invalid', 'the generic result lost its payload');
    const members = some.value.item;
    return { value: await members.call!.run(Number(args.with)), seed: members.call!.metadata.seed, none: batch.items[0]?.kind === 'none', null: members.empty === null, absent: !Object.hasOwn(batch, 'next'), extra: batch.label === 'retained' };
  },
  'gen.combinator_serve': unsupported,
  'gen.combinator_seen': unsupported,
  'gen.combinator_dial': async (args: Args) => {
    const d = new CombinatorDialled();
    d.client = await combinator.Client.dial(String(args.url), {}, undefined, {});
    next += 1;
    const handle = 'combcl' + String(next);
    handles.set(handle, d);
    return { handle };
  },
  'client.combinator_toolkit': async (args: Args) => {
    const d = lookup(args);
    const toolkit = await d.client.toolkit({ seed: Number(args.seed) });
    return { toolkit: d.hold(toolkit) };
  },
  'client.combinator_twice': async (args: Args) => {
    const toolkit = lookup(args).toolkit(String(args.toolkit));
    const add = Number(args.add);
    // A callable of this peer's, handed to a callable of the other's.
    const twice = await toolkit.twice(async (value: number) => value + add);
    // And the callable it answered, invoked here.
    return { value: await twice(Number(args.with)) };
  },
  'client.combinator_identity': async (args: Args) => {
    const toolkit = lookup(args).toolkit(String(args.toolkit));
    const identity = await toolkit.identity();
    return { value: await identity(Number(args.with)) };
  },
  'client.combinator_apply': async (args: Args) => {
    const toolkit = lookup(args).toolkit(String(args.toolkit));
    const factor = Number(args.factor);
    await toolkit.apply(async (value: number) => value * factor);
    return {};
  },
};
