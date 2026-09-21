/** One generic model and its generated adapters across local wire presentations. */
import { at, mount, type Wire } from '@nightseam/duplex';
import { forwardWire, wirePair, type Observer, type WireModelContext } from '@nightseam/runtime';
import { jsonAdapter } from '@nightseam/live';
import * as cell from './api/ts/cell-client/src/index.ts';
import * as binding from './api/ts/cell-binding/src/index.ts';
import { Inbox } from './server.ts';

type Args = Record<string, unknown>;
type Allocations = { peers: number; channels: number };

/** State and effects belong to the model, which knows neither carrier nor T. */
function model<T>(initial: T, noted: Inbox<T>, effects: { factories: number; mirrors: number; changes: number }): cell.ServerModel<T> {
  return remote => {
    effects.factories++;
    let value = initial;
    let revision = 0;
    return {
      methods: {
        put(params) { value = params.value; return ++revision; },
        get() { return value; },
        async roundTrip(params, context) {
          const result = await remote.methods.mirror(params, context);
          await remote.events.changed({ value: result }, context);
          return result;
        },
      },
      events: { noted(params) { noted.put(params.value); } },
    };
  };
}

function nested(wire: Wire, owned: Wire[]): Wire {
  const inner = mount(new Map([['inner', wire]]));
  const outer = mount(new Map([['outer', inner]]));
  owned.push(inner, outer);
  return at(at(outer, ['outer']), ['inner']);
}

async function local(args: Args): Promise<unknown> {
  const presentation = String(args.presentation ?? 'local');
  if (!['local', 'mounted', 'forwarded'].includes(presentation)) throw new Error('unknown wire presentation ' + presentation);
  const allocations: Allocations = { peers: 0, channels: 0 };
  const observer: Observer = { observe(event) {
    const type: string = event.type;
    if (type === 'connection.opened') allocations.peers++;
    if (type === 'channel.opened') allocations.channels++;
  } };
  const snapshot = (): Allocations => ({ ...allocations });
  const delta = (before: Allocations): Allocations => ({ peers: allocations.peers - before.peers, channels: allocations.channels - before.channels });
  const context = { options: { observer } };
  const adapter = jsonAdapter<string>({ type: 'string', validate: cell.validateWire });
  const noted = new Inbox<string>();
  const changed = new Inbox<string>();
  const effects = { factories: 0, mirrors: 0, changes: 0 };
  const local = binding.toWire(model('initial', noted, effects), context, adapter);
  const owned: Wire[] = [local];
  let detach: (() => void) | undefined;
  // Root carriers are prepared before views are measured. The observer counts
  // cumulative creations, so opening and closing a hidden carrier cannot pass.
  const bridge = presentation === 'forwarded' ? wirePair(context.options) : undefined;
  if (bridge) owned.push(...bridge);
  const beforeViews = snapshot();
  try {
    let presented: Wire = local;
    if (presentation === 'mounted') presented = nested(local, owned);
    if (bridge) {
      detach = forwardWire(bridge[1], local);
      presented = nested(bridge[0], owned);
    }
    const viewAllocations = delta(beforeViews);
    const beforeUse = snapshot();
    const factory = await binding.fromWire(presented, context, adapter);
    const server = factory({
      methods: { mirror(params) { effects.mirrors++; return params.value; } },
      events: { changed(params) { effects.changes++; changed.put(params.value); } },
    });
    const callContext: WireModelContext = { timeoutMs: Number(args.within_ms ?? 5000) };
    const revisions = [await server.methods.put({ value: 'first' }, callContext), await server.methods.put({ value: 'second' }, callContext)];
    const value = await server.methods.get({}, callContext);
    const reverse = await server.methods.roundTrip({ value: 'second' }, callContext);
    await server.events.noted({ value: 'first' }, callContext);
    const changedValue = await changed.take(callContext.timeoutMs!);
    const notedValue = await noted.take(callContext.timeoutMs!);
    if (changedValue === undefined || notedValue === undefined) throw new Error('wire event was not delivered');
    if (effects.factories !== 1 || effects.mirrors !== 1 || effects.changes !== 1) throw new Error('wire duplicated model construction or a callback');
    return { revisions, value, reverse, changed: changedValue, noted: notedValue, view_allocations: viewAllocations, use_allocations: delta(beforeUse) };
  } finally {
    detach?.();
    for (const wire of owned.reverse()) wire.close();
  }
}

export const wireCellOps: Record<string, (args: Args) => unknown | Promise<unknown>> = { 'gen.wire_local': local };
