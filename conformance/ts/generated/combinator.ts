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
 * The generated client and binding each hand this peer's functions to the
 * other and invoke the functions it answers.
 */
import * as combinator from './api/ts/combinator-client/src/index.ts';
import * as binding from './api/ts/combinator-binding/src/index.ts';
import { Served } from './server.ts';

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
/** Ordinary functions and containers implementing the declaration's server. */
export class CombinatorServer implements binding.Handler {
  readonly applied: number[] = [];
  name(): string { return 'combinator'; }
  pack: binding.Handler['pack'] = params => ({
    label: 'retained',
    items: [
      { kind: 'none' },
      { kind: 'some', value: { item: {
        call: { metadata: { seed: 7 }, run: async (value: number, options?: { signal?: AbortSignal }) => params.item(await params.item(value, options), options) },
        empty: null,
      } } },
    ],
  });
  toolkit(params: combinator.ToolkitRequest): combinator.Toolkit {
    return {
      twice: async once => async (value, options) => once(await once(value, options), options),
      identity: async () => async value => value,
      apply: async (each, options) => { this.applied.push(await each(params.seed, options)); },
    };
  }
}
const servers = new Map<string, { served: Served<binding.Remote>; server: CombinatorServer }>();
let next = 0;

export function resetCombinator(): void {
  for (const d of handles.values()) d.shutdown();
  for (const s of servers.values()) s.served.shutdown();
  handles.clear();
  servers.clear();
}

function lookup(args: Args): CombinatorDialled {
  const d = handles.get(String(args.on));
  if (!d) throw new CombinatorFailure('unknown_handle', String(args.on));
  return d;
}

export const combinatorOps: Record<string, (args: Args) => unknown | Promise<unknown>> = {
  'client.combinator_pack': async (args: Args) => {
    const add = Number(args.add);
    const batch = await lookup(args).client.pack({ item: async (value: number) => value + add });
    const some = batch.items[1];
    if (some?.kind !== 'some') throw new CombinatorFailure('invalid', 'the generic result lost its payload');
    const members = some.value.item;
    return { value: await members.call!.run(Number(args.with)), seed: members.call!.metadata.seed, none: batch.items[0]?.kind === 'none', null: members.empty === null, absent: !Object.hasOwn(batch, 'next'), extra: batch.label === 'retained' };
  },
  'gen.combinator_serve': async () => {
    const server = new CombinatorServer();
    const served = await new Served(socket => binding.serve(socket, {}, server, {}).then(peer => new binding.Remote(peer))).listen();
    const handle = 'combsrv' + String(++next);
    servers.set(handle, { served, server });
    return { handle, url: served.url };
  },
  'gen.combinator_seen': args => {
    const s = servers.get(String(args.on));
    if (!s) throw new CombinatorFailure('unknown_handle', String(args.on));
    return { applied: [...s.server.applied] };
  },
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
