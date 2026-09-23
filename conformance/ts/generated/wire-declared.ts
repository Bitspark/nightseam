/** Generated models behind declared composition, over a local pair, sockets and prepared channels. */
import { at, Declared, pipe, refusingOrigin, through } from '@nightseam/duplex';
import type { Endpoint, Message, Path, Wire } from '@bitspark/bitwire';
import { DuplexError, DuplexPeer, callWire, declarationDigest, forwardWire, wirePair } from '@nightseam/runtime';
import { liveOver, type LiveScope } from '@nightseam/live';
import { Tunnel } from '@nightseam/tunnel';
import * as cell from './api/ts/cell-client/src/index.ts';
import * as binding from './api/ts/cell-binding/src/index.ts';
import { Inbox, adapterContext } from './server.ts';
import { counts, model, state, withSlot, zero, type Slot } from './wire-cell.ts';

type Args = Record<string, unknown>;

class DeclaredRefusal extends Error {
  constructor() {
    super('refused by a declared admission policy');
  }
}

/** One guard's state: it counts its checks and can refuse the next one. */
class Gate {
  checks = 0;
  private refuse = false;
  admit(): void {
    this.checks++;
    if (this.refuse) {
      this.refuse = false;
      throw new DeclaredRefusal();
    }
  }
  refuseNext(): void {
    this.refuse = true;
  }
}

/** Consumer interception composed around access, as Bitwire ADR 0006 puts it.
 * It checks requests and events once per crossing and passes replies and
 * cancels through unchecked, so admitted work keeps them. */
function guard(gate: Gate, inner: Wire): Wire {
  return {
    send(path: Path, message: Message): void {
      if (message.frame.kind === 'request' || message.frame.kind === 'event') gate.admit();
      inner.send(path, message);
    },
  };
}

function deferred<T = void>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(yes => (resolve = yes));
  return { promise, resolve };
}

/** Answers the server's callback, holding it when asked so that a reply can be
 * delayed across a rebuild, or its cancellation observed. */
class Mirror<T> {
  private hold?: Promise<void>;
  entered = deferred();
  cancelled = deferred();
  holding(): () => void {
    const release = deferred();
    this.hold = release.promise;
    this.entered = deferred();
    this.cancelled = deferred();
    return () => release.resolve();
  }
  async mirror(params: { value: T }, context?: { signal?: AbortSignal }): Promise<T> {
    const hold = this.hold;
    this.hold = undefined;
    if (!hold) return params.value;
    this.entered.resolve();
    const signal = context?.signal;
    const aborted = new Promise<'aborted'>(resolve => {
      if (signal?.aborted) resolve('aborted');
      signal?.addEventListener('abort', () => resolve('aborted'), { once: true });
    });
    if ((await Promise.race([hold, aborted])) === 'aborted') {
      this.cancelled.resolve();
      throw new DuplexError('cancelled', 'callback cancelled');
    }
    return params.value;
  }
}

interface Descriptions {
  readonly model: Declared;
  readonly root: Declared;
}

/** The caller's declared composition of access to the server model: a guarded
 * root, then a guarded svc, then the model's complete operation domain as the
 * generated description spells it. The assembler keeps both descriptions; a
 * guard is opaque access, retained whole. */
class Tree {
  readonly root = new Gate();
  readonly svc = new Gate();
  build(target: Wire): Descriptions {
    return this.over(binding.declared(target));
  }
  over(model: Declared): Descriptions {
    return { model, root: Declared.compose(refusingOrigin, [['svc', guard(this.svc, model.bind())]]) };
  }
  /** Both descriptions recomposed from their parts, the model first: a
   * complete cut at every depth, retaining each origin, child and guard state. */
  rebuilt(parts: Descriptions): Descriptions {
    const { origin, children } = parts.model.decompose();
    return this.over(Declared.compose(origin, children));
  }
  access(parts: Descriptions, access: string): Wire {
    switch (access) {
      case 'direct':
        return guard(this.svc, parts.model.bind());
      case 'selected':
      case 'forwarded':
        return at(guard(this.root, parts.root.bind()), ['svc']);
      case 'reconstructed':
        return at(guard(this.root, this.rebuilt(parts).root.bind()), ['svc']);
    }
    throw new Error('unknown declared access ' + access);
  }
}

/** The error a call ends with, or undefined when it answered. */
async function failure(call: () => unknown): Promise<unknown> {
  try {
    await call();
    return undefined;
  } catch (error) {
    return error;
  }
}

/** A refusal this fixture's guard made, wherever the runtime wrapped it, and
 * otherwise the outermost public code. */
function code(error: unknown): string {
  const chain: unknown[] = [];
  for (let current = error; current; current = (current as { cause?: unknown }).cause) chain.push(current);
  if (chain.some(current => current instanceof DeclaredRefusal)) return 'refused';
  const first = chain.find(current => current instanceof DuplexError) as DuplexError | undefined;
  return first?.code ?? (error === undefined ? '' : 'internal');
}

/** Interprets the server model through declared access. Its own origins select
 * on target, where the server's callbacks and events arrive. */
class Client<T> {
  readonly tree = new Tree();
  readonly mirror = new Mirror<T>();
  readonly changed = new Inbox<T>();
  readonly cleanup: Array<() => void> = [];
  origin?: Endpoint;
  target?: Endpoint;
  parts?: Descriptions;
  server?: cell.Server<T>;
  scope?: LiveScope;
  context = adapterContext();
  readonly slot: Slot<T>;
  readonly access: string;
  constructor(slot: Slot<T>, access: string) {
    this.slot = slot;
    this.access = access;
  }

  /** Installs the interpretation on a carrier origin before its reads start. */
  prepare(origin: Endpoint, scope?: LiveScope): () => Promise<void> {
    this.origin = this.target = origin;
    this.scope = scope;
    this.context = adapterContext(scope);
    if (this.access === 'forwarded') {
      const [left, right] = wirePair();
      const detach = forwardWire(right, origin);
      this.cleanup.push(() => {
        detach();
        left.close();
        right.close();
      });
      this.target = left;
    }
    this.parts = this.tree.build(this.target);
    const presented = through(this.target, this.tree.access(this.parts, this.access));
    this.cleanup.push(() => presented.close());
    const preparation = binding.prepareFromWire(presented, this.context, this.slot.adapter);
    this.cleanup.push(() => preparation.close());
    return async () => {
      const factory = await preparation.complete();
      this.server = factory({
        methods: { mirror: (params, context) => this.mirror.mirror(params, context) },
        events: { changed: params => this.changed.put(params.value) },
      });
    };
  }

  shutdown(): void {
    for (const close of this.cleanup.splice(0).reverse()) close();
  }

  /** Every behavior this access promises, in one run: calls, a callback,
   * events both ways, a refused message, a reply delayed across a complete
   * rebuild and a rebind, a cancellation carried back through the callback,
   * and teardown that leaves the borrowed carrier usable. */
  async exercise(within: number): Promise<Record<string, unknown>> {
    const server = this.server!;
    const context = { timeoutMs: within, valueContext: this.scope?.owner() };
    const take = async (inbox: Inbox<T>, what: string): Promise<T> => {
      const value = await inbox.take(within);
      if (value === undefined) throw new Error(what + ' was not delivered');
      return value;
    };
    const first = await server.methods.put({ value: this.slot.make(1) }, context);
    const retained = this.slot.retain ? await server.methods.get({}, context) : undefined;
    const second = await server.methods.put({ value: this.slot.make(2) }, context);
    const value = await server.methods.get({}, context);
    const reverse = await server.methods.roundTrip({ value }, context);
    await server.events.noted({ value: this.slot.make(1) }, context);
    const change = await take(this.changed, 'changed event');

    // One refused message leaves the model, the access and the carrier usable.
    this.tree.svc.refuseNext();
    const refusal = await failure(() => server.methods.put({ value: this.slot.make(1) }, context));
    const after = await server.methods.get({}, context);

    // A reply delayed across a complete rebuild and a rebind of svc elsewhere.
    const established = { ...context, outgoingMeta: { access: this.access } };
    const release = this.mirror.holding();
    const delayed = Promise.resolve(server.methods.roundTrip({ value }, established));
    await this.mirror.entered.promise;
    const { origin, children } = this.tree.rebuilt(this.parts!).root.decompose();
    const elsewhere = binding.declared(at(this.target!, ['elsewhere']));
    const rebound = Declared.compose(
      origin,
      children.map(([key, child]) => [key, key === 'svc' ? guard(this.tree.svc, elsewhere.bind()) : child] as const),
    );
    const reboundError = await failure(() =>
      callWire(guard(this.tree.root, rebound.bind()), ['svc', 'get'], {}, { timeoutMs: within }),
    );
    release();
    const late = await delayed;
    const lateChange = await take(this.changed, 'delayed changed event');

    // A cancellation through the admitting access reaches the body, which
    // cancels its own callback: the cancel crosses the carrier both ways.
    this.mirror.holding();
    const controller = new AbortController();
    const cancelled = failure(() => server.methods.roundTrip({ value }, { ...established, signal: controller.signal }));
    await this.mirror.entered.promise;
    controller.abort();
    const callError = await cancelled;
    await this.mirror.cancelled.promise;

    const observe = (v: T) => this.slot.observe(v);
    const result: Record<string, unknown> = {
      revisions: [first, second],
      refusal: code(refusal),
      rebound: code(reboundError),
      cancelled: code(callError),
      checks: { root: this.tree.root.checks, svc: this.tree.svc.checks },
      value: await observe(value),
      reverse: await observe(reverse),
      changed: await observe(change),
      after_refusal: await observe(after),
      delayed: await observe(late),
      delayed_changed: await observe(lateChange),
    };
    if (retained !== undefined) result.retained = await observe(retained);
    return result;
  }

  /** Releases this side's bindings and detaches the interpretation. The
   * borrowed carrier stays usable: a fresh interpretation completes over it. */
  async close(within: number): Promise<Record<string, unknown>> {
    let released = counts(this.scope);
    if (this.scope) {
      this.scope.owner().release();
      released = await zero(this.scope, within);
    }
    this.shutdown();
    const fresh = binding.prepareFromWire(this.origin!, this.context, this.slot.adapter);
    try {
      await fresh.complete();
      return { released_counts: released, borrowed: true };
    } catch {
      return { released_counts: released, borrowed: false };
    } finally {
      fresh.close();
    }
  }
}

async function local<T>(args: Args, slot: Slot<T>): Promise<unknown> {
  const within = Number(args.within_ms ?? 5000);
  const client = new Client(slot, String(args.access));
  const peers: DuplexPeer[] = [];
  let callerScope: LiveScope | undefined, calleeScope: LiveScope | undefined;
  if (slot.adapter.needsContext) {
    const caller = new DuplexPeer();
    const callee = new DuplexPeer({ role: 'server' });
    peers.push(caller, callee);
    callerScope = liveOver(caller);
    calleeScope = liveOver(callee);
    const [a, b] = pipe();
    await Promise.all([caller.attach(a), callee.attach(b)]);
  }
  const effects = state(slot.make(1));
  const served = binding.toWire(model(effects), adapterContext(calleeScope), slot.adapter);
  try {
    await client.prepare(served, callerScope)();
    const result = await client.exercise(within);
    const noted = await effects.noted.take(within);
    if (noted === undefined) throw new Error('noted event was not delivered');
    result.noted = await slot.observe(noted);
    result.server = { revision: effects.revision, meta: effects.meta ?? {} };
    return { ...result, ...(await client.close(within)) };
  } finally {
    client.shutdown();
    calleeScope?.owner().release();
    served.close();
    for (const peer of peers) peer.close();
  }
}

interface Active {
  exercise(within: number): Promise<unknown>;
  settle(within: number): Promise<unknown>;
  close(): void;
}

/** One declared caller over a physical carrier. */
class Dialled<T> implements Active {
  readonly client: Client<T>;
  readonly args: Args;
  readonly slot: Slot<T>;
  peer?: DuplexPeer;
  constructor(values: Args, slot: Slot<T>) {
    this.args = values;
    this.slot = slot;
    this.client = new Client(slot, String(values.access));
  }
  async start(): Promise<this> {
    const carrier = String(args(this.args, 'carrier'));
    const peer = new DuplexPeer({ role: 'client' });
    this.peer = peer;
    let complete: (() => Promise<void>) | undefined;
    const install = (prepared: DuplexPeer) => {
      const scope = this.slot.adapter.needsContext ? liveOver(prepared) : undefined;
      complete = this.client.prepare(prepared.wire(), scope);
    };
    try {
      if (carrier === 'socket') install(peer);
      else if (carrier !== 'channel') throw new Error('unknown declared carrier ' + carrier);
      const tunnel = carrier === 'channel' ? new Tunnel(peer) : undefined;
      await peer.connect(String(this.args.url));
      if (tunnel) {
        const digest = declarationDigest(cell.validateWire, { T: this.slot.adapter.binding });
        await tunnel.open('cell', digest, { prepare: install });
      }
      await complete!();
      return this;
    } catch (error) {
      this.close();
      throw error;
    }
  }
  exercise(within: number): Promise<unknown> {
    return this.client.exercise(within);
  }
  settle(within: number): Promise<unknown> {
    return this.client.close(within);
  }
  close(): void {
    this.client.shutdown();
    this.peer?.close();
  }
}

function args(values: Args, name: string): unknown {
  if (!(name in values)) throw new Error('missing argument ' + name);
  return values[name];
}

const dialled = new Map<string, Active>();
let next = 0;
const within = (values: Args): number => Number(values.within_ms ?? 5000);

function handleOf(values: Args): Active {
  const handle = dialled.get(String(values.on));
  if (!handle) throw new Error('unknown declared handle ' + String(values.on));
  return handle;
}

export function resetWireDeclared(): void {
  for (const handle of dialled.values()) handle.close();
  dialled.clear();
}

export const wireDeclaredOps: Record<string, (args: Args) => unknown | Promise<unknown>> = {
  'gen.wire_declared': values => withSlot(values.slot, slot => local(values, slot)),
  'gen.wire_declared_dial': async values => {
    const started = await withSlot<Promise<Active>>(values.slot, slot => new Dialled(values, slot).start());
    const handle = 'declared' + String(++next);
    dialled.set(handle, started);
    return { handle };
  },
  'gen.wire_declared_exercise': values => handleOf(values).exercise(within(values)),
  'gen.wire_declared_close': values => handleOf(values).settle(within(values)),
};
