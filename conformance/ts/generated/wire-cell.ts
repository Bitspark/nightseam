/** One generic model and its generated adapters across local wire presentations. */
import { at, mount, type Wire } from '@nightseam/duplex';
import { DuplexPeer, forwardWire, jsonAdapter, wirePair, type Observer, type WebSocketLike, type WireModelContext } from '@nightseam/runtime';
import { Tunnel } from '@nightseam/tunnel';
import * as cell from './api/ts/cell-client/src/index.ts';
import * as binding from './api/ts/cell-binding/src/index.ts';
import { Inbox, Served } from './server.ts';

type Args = Record<string, unknown>;
type Allocations = { peers: number; channels: number };
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
  const effects = state('initial');
  const local = binding.toWire(model(effects), context, adapter);
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
    const server = factory(opposite(effects));
    const callContext: WireModelContext = { timeoutMs: Number(args.within_ms ?? 5000) };
    const revisions = [await server.methods.put({ value: 'first' }, callContext), await server.methods.put({ value: 'second' }, callContext)];
    const value = await server.methods.get({}, callContext);
    const reverse = await server.methods.roundTrip({ value: 'second' }, callContext);
    await server.events.noted({ value: 'first' }, callContext);
    const changedValue = await effects.changed.take(callContext.timeoutMs!);
    const notedValue = await effects.noted.take(callContext.timeoutMs!);
    if (changedValue === undefined || notedValue === undefined) throw new Error('wire event was not delivered');
    if (effects.factories !== 1 || effects.mirrors !== 1 || effects.changes !== 1) throw new Error('wire duplicated model construction or a callback');
    return { revisions, value, reverse, changed: changedValue, noted: notedValue, view_allocations: viewAllocations, use_allocations: delta(beforeUse) };
  } finally {
    detach?.();
    for (const wire of owned.reverse()) wire.close();
  }
}

/** The test owns the chosen carrier; the generic model and adapter do not. */
class WireEndpoint {
  readonly state = state('initial');
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
  server?: cell.Server<string>;
  viewAllocations: Allocations = { peers: 0, channels: 0 };
  useBaseline: Allocations = { peers: 0, channels: 0 };
  setupAllocations: Allocations = { peers: 0, channels: 0 };
  constructor(args: Args, serving: boolean) {
    this.carrier = String(args.carrier ?? 'socket');
    this.presentation = String(args.presentation ?? 'local');
    this.serving = serving;
    if (args.slot !== undefined && args.slot !== 'string') throw new Error('unknown cell slot ' + String(args.slot));
    if (!['socket', 'channel'].includes(this.carrier)) throw new Error('unknown wire carrier ' + this.carrier);
    if (!['local', 'mounted', 'forwarded'].includes(this.presentation)) throw new Error('unknown wire presentation ' + this.presentation);
  }
  snapshot(): Allocations { return { ...this.allocations }; }
  delta(before: Allocations): Allocations {
    return { peers: this.allocations.peers - before.peers, channels: this.allocations.channels - before.channels };
  }
  install(peer: DuplexPeer): void {
    const context = { options: { observer: this.observer } };
    const adapter = jsonAdapter<string>({ type: 'string', validate: cell.validateWire });
    const local = this.serving
      ? binding.toWire(model(this.state), context, adapter)
      : cell.toWire(remote => { this.state.factories++; this.server = remote; return opposite(this.state); }, context, adapter);
    this.owned.push(local);
    const beforeViews = this.snapshot();
    let presented = peer.wire();
    if (this.presentation !== 'local') {
      const mounted = mount(new Map([['route', presented]]));
      this.owned.push(mounted);
      // Both sides use the same relative prefix. It remains in the serialized
      // method/event name while the fixture mount consumes only its route key.
      presented = at(mounted, ['route', 'outer', 'inner']);
    }
    if (this.presentation === 'forwarded') {
      const [access, forwarding] = wirePair(context.options);
      this.owned.push(access, forwarding);
      this.detaches.push(forwardWire(forwarding, presented));
      presented = access;
    }
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
  counts(): { exports: number; imports: number } { return { exports: 0, imports: 0 }; }
  allocationsReport(): unknown {
    return { setup_allocations: this.setupAllocations, view_allocations: this.viewAllocations, use_allocations: this.delta(this.useBaseline) };
  }
  async exercise(within: number): Promise<unknown> {
    const server = this.server;
    if (!server) throw new Error('wire handle has no remote server model');
    const context: WireModelContext = { timeoutMs: within };
    const revisions = [await server.methods.put({ value: 'first' }, context), await server.methods.put({ value: 'second' }, context)];
    const value = await server.methods.get({}, context);
    const reverse = await server.methods.roundTrip({ value: 'second' }, context);
    await server.events.noted({ value: 'first' }, context);
    const changed = await this.state.changed.take(within);
    if (changed === undefined) throw new Error('changed event was not delivered');
    if (this.state.factories !== 1 || this.state.mirrors !== 1 || this.state.changes !== 1) throw new Error('wire duplicated model construction or a callback');
    return { revisions, value, reverse, changed, ...(this.allocationsReport() as object), counts: this.counts() };
  }
  async inspect(within: number): Promise<unknown> {
    if (!this.serving) throw new Error('wire handle is not a server');
    const noted = this.state.lastNoted ?? await this.state.noted.take(within);
    if (noted === undefined) throw new Error('noted event was not delivered');
    if (this.state.factories !== 1) throw new Error('wire duplicated model construction');
    return { revision: this.state.revision, value: this.state.value, noted, ...(this.allocationsReport() as object), counts: this.counts() };
  }
}

interface Handle { endpoint?: WireEndpoint; served?: Served<WireEndpoint> }
const handles = new Map<string, Handle>();
let next = 0;
const within = (args: Args): number => Number(args.within_ms ?? 5000);
async function endpoint(args: Args): Promise<WireEndpoint> {
  const handle = handles.get(String(args.on));
  if (!handle) throw new Error('unknown wire handle ' + String(args.on));
  const value = handle.endpoint ?? await handle.served?.remote(within(args));
  if (!value) throw new Error('no wire client connected');
  return value;
}
export function resetWireCells(): void {
  for (const handle of handles.values()) { handle.endpoint?.close(); handle.served?.shutdown(); }
  handles.clear();
}
export const wireCellOps: Record<string, (args: Args) => unknown | Promise<unknown>> = {
  'gen.wire_local': local,
  'gen.wire_serve': async args => {
    const served = await new Served(socket => new WireEndpoint(args, true).start(socket)).listen();
    const handle = 'wire' + String(++next);
    handles.set(handle, { served });
    return { handle, url: served.url };
  },
  'gen.wire_dial': async args => {
    const connected = await new WireEndpoint(args, false).start(String(args.url));
    const handle = 'wire' + String(++next);
    handles.set(handle, { endpoint: connected });
    return { handle };
  },
  'gen.wire_exercise': async args => (await endpoint(args)).exercise(within(args)),
  'gen.wire_inspect': async args => (await endpoint(args)).inspect(within(args)),
  // Scalar slots have no conversion environment and acquire no live bindings.
  'gen.wire_release': async args => { await endpoint(args); return {}; },
  'gen.wire_counts': async args => (await endpoint(args)).counts(),
};
