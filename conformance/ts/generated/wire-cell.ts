/** One generic model and its generated adapters across wire presentations and carriers. */
import { at, mount, pipe, type Wire } from '@nightseam/duplex';
import { DuplexPeer, forwardWire, jsonAdapter, wirePair, type Observer, type ValueAdapter, type WebSocketLike } from '@nightseam/runtime';
import { liveOver, type LiveScope } from '@nightseam/live';
import { Tunnel } from '@nightseam/tunnel';
import * as cell from './api/ts/cell-client/src/index.ts';
import * as binding from './api/ts/cell-binding/src/index.ts';
import * as combinator from './api/ts/combinator-client/src/index.ts';
import * as boxes from './api/ts/boxes-client/src/index.ts';
import { Inbox, Served, adapterContext } from './server.ts';

type Args = Record<string, unknown>;
type Allocations = { peers: number; channels: number };
type Counts = { exports: number; imports: number };
interface Slot<T> {
  adapter: ValueAdapter<T>;
  make(add: number): T;
  observe(value: T): Promise<unknown>;
}
const unary = (add: number): combinator.Unary => async value => value + add;
const stringSlot: Slot<string> = {
  adapter: jsonAdapter<string>({ type: 'string', validate: cell.validateWire }),
  make: add => add === 1 ? 'first' : 'second',
  observe: async value => value,
};
const unarySlot: Slot<combinator.Unary> = {
  adapter: combinator.adapterUnary(), make: unary, observe: async value => value(5),
};
const factorySlot: Slot<combinator.Factory> = {
  adapter: combinator.adapterFactory(),
  make: add => async callback => async value => (await callback(value)) + add,
  observe: async value => {
    let calls = 0;
    const result = await value(async number => { calls++; return number + 3; });
    const observed = await result(5);
    if (calls !== 1) throw new Error('higher-order callback did not run exactly once');
    return observed;
  },
};
type Nested = boxes.Page<combinator.Bundle<combinator.Unary>>;
const nestedSlot: Slot<Nested> = {
  adapter: boxes.adapterPage(combinator.adapterBundle(combinator.adapterUnary())),
  make: add => ({ items: [{ metadata: { seed: unary(add) }, run: unary(10 * add) }], label: 'kept' }),
  observe: async value => {
    if (value.items.length !== 1) throw new Error('nested slot lost its sole item');
    return { seed: await value.items[0]!.metadata.seed(5), run: await value.items[0]!.run(5), label: value.label, next_absent: !Object.hasOwn(value, 'next') };
  },
};
/** Slot selection belongs to fixture assembly, outside the generic model. */
function withSlot<R>(name: unknown, run: <T>(slot: Slot<T>) => R): R {
  switch (name ?? 'string') {
    case 'string': return run(stringSlot);
    case 'unary': return run(unarySlot);
    case 'factory': return run(factorySlot);
    case 'nested': return run(nestedSlot);
    default: throw new Error('unknown cell slot ' + String(name));
  }
}
const counts = (scope?: LiveScope): Counts => scope?.counts() ?? { exports: 0, imports: 0 };
async function zero(scope: LiveScope | undefined, within: number): Promise<Counts> {
  const deadline = Date.now() + within;
  for (;;) {
    const value = counts(scope);
    if (value.exports === 0 && value.imports === 0) return value;
    if (Date.now() >= deadline) throw new Error('wire cell retained bindings: ' + JSON.stringify(value));
    await new Promise(resolve => setTimeout(resolve, 1));
  }
}
interface CellState<T> {
  value: T;
  revision: number;
  factories: number;
  mirrors: number;
  changes: number;
  noted: Inbox<T>;
  changed: Inbox<T>;
  lastNoted?: T;
}
function state<T>(value: T): CellState<T> {
  return { value, revision: 0, factories: 0, mirrors: 0, changes: 0, noted: new Inbox<T>(), changed: new Inbox<T>() };
}

/** State and effects belong to the model, which knows neither carrier nor T. */
function model<T>(state: CellState<T>): cell.ServerModel<T> {
  return remote => {
    state.factories++;
    return {
      methods: {
        put(params) { state.value = params.value; return ++state.revision; },
        get() { return state.value; },
        async roundTrip(params, context) {
          const result = await remote.methods.mirror(params, context);
          await remote.events.changed({ value: result }, context);
          return result;
        },
      },
      events: { noted(params) { state.lastNoted = params.value; state.noted.put(params.value); } },
    };
  };
}
function opposite<T>(state: CellState<T>): cell.Client<T> {
  return {
    methods: { mirror(params) { state.mirrors++; return params.value; } },
    events: { changed(params) { state.changes++; state.changed.put(params.value); } },
  };
}

function nested(wire: Wire, owned: Wire[]): Wire {
  const inner = mount(new Map([['inner', wire]]));
  const outer = mount(new Map([['outer', inner]]));
  owned.push(inner, outer);
  return at(at(outer, ['outer']), ['inner']);
}

function carrierView(wire: Wire, presentation: string, observer: Observer, owned: Wire[], detaches: Array<() => void>): Wire {
  let presented = wire;
  if (presentation !== 'local') {
    const mounted = mount(new Map([['route', presented]]));
    owned.push(mounted);
    presented = at(mounted, ['route', 'outer', 'inner']);
  }
  if (presentation === 'forwarded') {
    const [access, forwarding] = wirePair({ observer });
    owned.push(access, forwarding);
    detaches.push(forwardWire(forwarding, presented));
    presented = access;
  }
  return presented;
}

async function local<T>(args: Args, slot: Slot<T>): Promise<unknown> {
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
  const peers: DuplexPeer[] = [];
  let callerScope: LiveScope | undefined, calleeScope: LiveScope | undefined;
  if (slot.adapter.needsContext) {
    const caller = new DuplexPeer({ observer });
    const callee = new DuplexPeer({ role: 'server', observer });
    peers.push(caller, callee);
    callerScope = liveOver(caller);
    calleeScope = liveOver(callee);
    const [a, b] = pipe();
    await Promise.all([caller.attach(a), callee.attach(b)]);
  }
  const first = slot.make(1), second = slot.make(2);
  const context = adapterContext(calleeScope, { observer });
  const effects = state(first);
  const local = binding.toWire(model(effects), context, slot.adapter);
  const owned: Wire[] = [local];
  let detach: (() => void) | undefined;
  // Root carriers are prepared before views are measured. The observer counts
  // cumulative creations, so opening and closing a hidden carrier cannot pass.
  const bridge = presentation === 'forwarded' ? wirePair(context.options) : undefined;
  if (bridge) owned.push(...bridge);
  const setupAllocations = snapshot();
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
    const factory = await binding.fromWire(presented, adapterContext(callerScope, { observer }), slot.adapter);
    const server = factory(opposite(effects));
    const callContext = { timeoutMs: Number(args.within_ms ?? 5000), valueContext: callerScope?.owner() };
    const revisions = [await server.methods.put({ value: first }, callContext), await server.methods.put({ value: second }, callContext)];
    const returned = await server.methods.get({}, callContext);
    const mirrored = await server.methods.roundTrip({ value: returned }, callContext);
    await server.events.noted({ value: first }, callContext);
    const changedValue = await effects.changed.take(callContext.timeoutMs!);
    const notedValue = await effects.noted.take(callContext.timeoutMs!);
    if (changedValue === undefined || notedValue === undefined) throw new Error('wire event was not delivered');
    if (effects.factories !== 1 || effects.mirrors !== 1 || effects.changes !== 1) throw new Error('wire duplicated model construction or a callback');
    const value = await slot.observe(returned), reverse = await slot.observe(mirrored);
    const changed = await slot.observe(changedValue), noted = await slot.observe(notedValue);
    const held = { caller: counts(callerScope), callee: counts(calleeScope) };
    callerScope?.owner().release();
    calleeScope?.owner().release();
    const released = { caller: await zero(callerScope, callContext.timeoutMs), callee: await zero(calleeScope, callContext.timeoutMs) };
    return { revisions, value, reverse, changed, noted, setup_allocations: setupAllocations, view_allocations: viewAllocations, use_allocations: delta(beforeUse), counts: held, released_counts: released };
  } finally {
    detach?.();
    for (const wire of owned.reverse()) wire.close();
    for (const peer of peers) peer.close();
  }
}

/** The test owns the chosen carrier; the generic model and adapter do not. */
class WireEndpoint<T> {
  readonly state: CellState<T>;
  readonly slot: Slot<T>;
  readonly first: T;
  readonly second: T;
  readonly allocations: Allocations = { peers: 0, channels: 0 };
  readonly observer: Observer = { observe: event => {
    const type: string = event.type;
    if (type === 'connection.opened') this.allocations.peers++;
    if (type === 'channel.opened') this.allocations.channels++;
  } };
  readonly owned: Wire[] = [];
  readonly detaches: Array<() => void> = [];
  readonly carrier: string;
  readonly presentation: string;
  readonly serving: boolean;
  outer?: DuplexPeer;
  scope?: LiveScope;
  server?: cell.Server<T>;
  viewAllocations: Allocations = { peers: 0, channels: 0 };
  useBaseline: Allocations = { peers: 0, channels: 0 };
  setupAllocations: Allocations = { peers: 0, channels: 0 };
  constructor(args: Args, serving: boolean, slot: Slot<T>) {
    this.slot = slot;
    this.first = slot.make(1);
    this.second = slot.make(2);
    this.state = state(this.first);
    this.carrier = String(args.carrier ?? 'socket');
    this.presentation = String(args.presentation ?? 'local');
    this.serving = serving;
    if (!['socket', 'channel'].includes(this.carrier)) throw new Error('unknown wire carrier ' + this.carrier);
    if (!['local', 'mounted', 'forwarded'].includes(this.presentation)) throw new Error('unknown wire presentation ' + this.presentation);
  }
  snapshot(): Allocations { return { ...this.allocations }; }
  delta(before: Allocations): Allocations {
    return { peers: this.allocations.peers - before.peers, channels: this.allocations.channels - before.channels };
  }
  install(peer: DuplexPeer): void {
    if (this.slot.adapter.needsContext) this.scope = liveOver(peer);
    const context = adapterContext(this.scope, { observer: this.observer });
    const adapter = this.slot.adapter;
    const local = this.serving
      ? binding.toWire(model(this.state), context, adapter)
      : cell.toWire(remote => { this.state.factories++; this.server = remote; return opposite(this.state); }, context, adapter);
    this.owned.push(local);
    const beforeViews = this.snapshot();
    // Both sides preserve the prefix in the serialized method/event name.
    const presented = carrierView(peer.wire(), this.presentation, this.observer, this.owned, this.detaches);
    this.detaches.push(forwardWire(presented, local));
    this.viewAllocations = this.delta(beforeViews);
  }
  async start(socketOrURL: WebSocketLike | string): Promise<this> {
    const peer = new DuplexPeer({ role: this.serving ? 'server' : 'client', observer: this.observer });
    this.outer = peer;
    peer.onClose(() => this.releaseWires());
    const tunnel = this.carrier === 'channel' ? new Tunnel(peer) : undefined;
    try {
      if (!tunnel) this.install(peer);
      if (typeof socketOrURL === 'string') await peer.connect(socketOrURL);
      else await peer.attach(socketOrURL);
      if (tunnel) {
        const options = { observer: this.observer, prepare: (prepared: DuplexPeer) => this.install(prepared) };
        const channel = this.serving ? await tunnel.accept(options) : await tunnel.open('wire-cell', options);
        this.owned.push(channel);
      }
      this.setupAllocations = this.snapshot();
      const expectedPeers = tunnel ? 2 : 1;
      const expectedChannels = tunnel ? 1 : 0;
      if (this.allocations.peers !== expectedPeers || this.allocations.channels !== expectedChannels) throw new Error('carrier setup allocated an unexpected peer or channel');
      this.useBaseline = this.snapshot();
      return this;
    } catch (error) { this.close(); throw error; }
  }
  private releaseWires(): void {
    for (const detach of this.detaches.splice(0).reverse()) detach();
    for (const wire of this.owned.splice(0).reverse()) wire.close();
  }
  close(): void { this.releaseWires(); this.outer?.close(); }
  counts(): Counts { return counts(this.scope); }
  release(): void { this.scope?.owner().release(); }
  async awaitZero(within: number): Promise<Counts> { return zero(this.scope, within); }
  allocationsReport() {
    return { setup_allocations: this.setupAllocations, view_allocations: this.viewAllocations, use_allocations: this.delta(this.useBaseline) };
  }
  async exercise(within: number): Promise<unknown> {
    const server = this.server;
    if (!server) throw new Error('wire handle has no remote server model');
    const context = { timeoutMs: within, valueContext: this.scope?.owner() };
    const revisions = [await server.methods.put({ value: this.first }, context), await server.methods.put({ value: this.second }, context)];
    const returned = await server.methods.get({}, context);
    const mirrored = await server.methods.roundTrip({ value: returned }, context);
    await server.events.noted({ value: this.first }, context);
    const changed = await this.state.changed.take(within);
    if (changed === undefined) throw new Error('changed event was not delivered');
    if (this.state.factories !== 1 || this.state.mirrors !== 1 || this.state.changes !== 1) throw new Error('wire duplicated model construction or a callback');
    const value = await this.slot.observe(returned), reverse = await this.slot.observe(mirrored);
    const observed = await this.slot.observe(changed);
    this.checkLiveCounts();
    return { revisions, value, reverse, changed: observed, ...this.allocationsReport(), counts: this.counts() };
  }
  async inspect(within: number): Promise<unknown> {
    if (!this.serving) throw new Error('wire handle is not a server');
    const noted = this.state.lastNoted ?? await this.state.noted.take(within);
    if (noted === undefined) throw new Error('noted event was not delivered');
    if (this.state.factories !== 1) throw new Error('wire duplicated model construction');
    const value = await this.slot.observe(this.state.value), observedNoted = await this.slot.observe(noted);
    this.checkLiveCounts();
    return { revision: this.state.revision, value, noted: observedNoted, ...this.allocationsReport(), counts: this.counts() };
  }
  private checkLiveCounts(): void {
    const value = this.counts();
    if (this.slot.adapter.needsContext && (!value.exports || !value.imports)) throw new Error('live slot did not exercise both conversion directions');
  }
}

interface ActiveEndpoint {
  start(socketOrURL: WebSocketLike | string): Promise<ActiveEndpoint>;
  exercise(within: number): Promise<unknown>;
  inspect(within: number): Promise<unknown>;
  release(): void;
  awaitZero(within: number): Promise<Counts>;
  close(): void;
}

/** The middle imports a whole model factory and exports that exact factory
 * under another scope. It has no operation implementation or slot forwarding. */
class ModelBridge {
  readonly allocations: Allocations = { peers: 0, channels: 0 };
  readonly observer: Observer = { observe: event => {
    const type: string = event.type;
    if (type === 'connection.opened') this.allocations.peers++;
    if (type === 'channel.opened') this.allocations.channels++;
  } };
  readonly owned: Wire[] = [];
  readonly detaches: Array<() => void> = [];
  origin?: DuplexPeer;
  destination?: DuplexPeer;
  originScope?: LiveScope;
  destinationScope?: LiveScope;
  served?: Served<{ close(): void }>;
  useBaseline: Allocations = { peers: 0, channels: 0 };
  setupAllocations: Allocations = { peers: 0, channels: 0 };
  viewAllocations: Allocations = { peers: 0, channels: 0 };
  released = false;
  select(peer: DuplexPeer, presentation: string): Wire {
    const before = { ...this.allocations };
    const selected = carrierView(peer.wire(), presentation, this.observer, this.owned, this.detaches);
    this.viewAllocations.peers += this.allocations.peers - before.peers;
    this.viewAllocations.channels += this.allocations.channels - before.channels;
    return selected;
  }
  async start(args: Args): Promise<string> {
    if (args.slot !== 'factory') throw new Error('the model bridge witness uses the declared Factory slot');
    const presentation = String(args.presentation);
    if (!['mounted', 'forwarded'].includes(presentation)) throw new Error('unknown model bridge presentation ' + presentation);
    try {
      const origin = new DuplexPeer({ observer: this.observer });
      this.origin = origin;
      this.originScope = liveOver(origin);
      const source = this.select(origin, presentation);
      const imported = await binding.fromWire(source, adapterContext(this.originScope, { observer: this.observer }), factorySlot.adapter);
      await origin.connect(String(args.origin));
      this.served = await new Served(async socket => {
        const destination = new DuplexPeer({ role: 'server', observer: this.observer });
        this.destination = destination;
        this.destinationScope = liveOver(destination);
        const output = binding.toWire(imported, adapterContext(this.destinationScope, { observer: this.observer }), factorySlot.adapter);
        this.owned.push(output);
        const selected = this.select(destination, presentation);
        this.detaches.push(forwardWire(selected, output));
        await destination.attach(socket);
        this.setupAllocations = { ...this.allocations };
        if (this.allocations.peers !== 2 || this.allocations.channels !== 0) throw new Error('model bridge allocated an unexpected carrier');
        this.useBaseline = { ...this.allocations };
        return { close: () => destination.close() };
      }).listen();
      return this.served.url;
    } catch (error) { this.close(); throw error; }
  }
  async counts(within: number): Promise<unknown> {
    if (!await this.served?.remote(within)) throw new Error('model bridge has no destination');
    const origin = this.released ? await zero(this.originScope, within) : counts(this.originScope);
    const destination = this.released ? await zero(this.destinationScope, within) : counts(this.destinationScope);
    return { origin, destination, setup_allocations: this.setupAllocations, view_allocations: this.viewAllocations,
      use_allocations: { peers: this.allocations.peers - this.useBaseline.peers, channels: this.allocations.channels - this.useBaseline.channels } };
  }
  release(): void {
    this.originScope?.owner().release();
    this.destinationScope?.owner().release();
    this.released = true;
  }
  close(): void {
    for (const detach of this.detaches.splice(0).reverse()) detach();
    for (const wire of this.owned.splice(0).reverse()) wire.close();
    this.origin?.close();
    this.destination?.close();
    this.served?.shutdown();
  }
}

const makeEndpoint = (args: Args, serving: boolean): ActiveEndpoint => withSlot<ActiveEndpoint>(args.slot, slot => new WireEndpoint(args, serving, slot));
interface Handle { endpoint?: ActiveEndpoint; served?: Served<ActiveEndpoint> }
const handles = new Map<string, Handle>();
const bridges = new Map<string, ModelBridge>();
let next = 0;
const within = (args: Args): number => Number(args.within_ms ?? 5000);
async function endpoint(args: Args): Promise<ActiveEndpoint> {
  const handle = handles.get(String(args.on));
  if (!handle) throw new Error('unknown wire handle ' + String(args.on));
  const value = handle.endpoint ?? await handle.served?.remote(within(args));
  if (!value) throw new Error('no wire client connected');
  return value;
}
export function resetWireCells(): void {
  for (const handle of handles.values()) { handle.endpoint?.close(); handle.served?.shutdown(); }
  handles.clear();
  for (const bridge of bridges.values()) bridge.close();
  bridges.clear();
}
function bridge(args: Args): ModelBridge {
  const result = bridges.get(String(args.on));
  if (!result) throw new Error('unknown model bridge ' + String(args.on));
  return result;
}
export const wireCellOps: Record<string, (args: Args) => unknown | Promise<unknown>> = {
  'gen.wire_local': args => withSlot(args.slot, slot => local(args, slot)),
  'gen.wire_serve': async args => {
    const served = await new Served(socket => makeEndpoint(args, true).start(socket)).listen();
    const handle = 'wire' + String(++next);
    handles.set(handle, { served });
    return { handle, url: served.url };
  },
  'gen.wire_dial': async args => {
    const connected = await makeEndpoint(args, false).start(String(args.url));
    const handle = 'wire' + String(++next);
    handles.set(handle, { endpoint: connected });
    return { handle };
  },
  'gen.wire_exercise': async args => (await endpoint(args)).exercise(within(args)),
  'gen.wire_inspect': async args => (await endpoint(args)).inspect(within(args)),
  'gen.wire_release': async args => { (await endpoint(args)).release(); return {}; },
  'gen.wire_counts': async args => (await endpoint(args)).awaitZero(within(args)),
  'gen.wire_bridge': async args => {
    const bridge = new ModelBridge();
    const url = await bridge.start(args);
    const handle = 'wirebridge' + String(++next);
    bridges.set(handle, bridge);
    return { handle, url };
  },
  'gen.wire_bridge_counts': args => bridge(args).counts(within(args)),
  'gen.wire_bridge_release': args => { bridge(args).release(); return {}; },
};
