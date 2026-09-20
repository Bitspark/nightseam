/**
 * Callable values across one connection: a scope over a peer, bindings
 * exported from it, and references that name them inside an ordinary payload.
 * It is the third level of the declaration language — data, RPC, live — and
 * the runtime half of it.
 *
 * A callable is one unary function, so a binding is one function made
 * addressable from the other side of one connection. The layer speaks as a
 * layer speaks: a reserved prefix, live., and ordinary frames of the profile
 * beneath — live.invoke, a request, and live.release, an event — exactly as the
 * tunnel speaks channel.open. Nothing of this reaches the envelope and no frame
 * kind is added.
 *
 * What a reference names is a binding of one scope, and a scope is one
 * connection. A scope mints a random nonce when it is made and every binding id
 * carries it, so a binding id of one scope is not a binding id of any other and
 * a token detached from its connection resolves nowhere. A Reference has no
 * public constructor: it comes from an export or from decode, and nothing else
 * mints one.
 *
 * The layer proves which binding of which contract, and never who may call it.
 */
import { DuplexError, type DuplexPeer } from '@nightseam/runtime';

/**
 * What a scope tells the observer of the peer it runs over. They are members of
 * the runtime's registry, so a consumer that imports this package switches over
 * them beside the runtime's own events; a scope takes no observer of its own
 * and observes through its peer or not at all. They say which binding of which
 * contract, and never what it was asked or what it answered.
 */
declare module '@nightseam/runtime' {
  interface ObserverEvents {
    /** A binding exists, on the side that exported it. */
    'live.exported': { type: 'live.exported'; at: Date; contract: string; binding: string };
    /** An attachment to a binding was made here, once per binding however often it arrives. */
    'live.imported': { type: 'live.imported'; at: Date; contract: string; binding: string };
    /** A binding ended, once and whatever ended it — a release here, or the other side's word that it released. */
    'live.released': { type: 'live.released'; at: Date; contract: string; binding: string };
    /** An export, an import or an invocation that did not happen, on the side that refused it. */
    'live.refused': { type: 'live.refused'; at: Date; contract: string; code: string; reason: string };
  }
}

/** The request that invokes a binding, and the event that releases one. */
export const INVOKE_METHOD = 'live.invoke';
export const RELEASE_EVENT = 'live.release';

/** The public codes this layer refuses with, as the Go scope exports them. */
export const CONTRACT_INVALID = 'contract_invalid';
export const CONTRACT_MISMATCH = 'contract_mismatch';
export const REFERENCE_UNKNOWN = 'reference_unknown';
export const REFERENCE_FOREIGN = 'reference_foreign';
export const REFERENCE_RELEASED = 'reference_released';
export const SCOPE_CLOSED = 'scope_closed';
export const TOO_MANY_EXPORTS = 'too_many_exports';
export const TOO_MANY_IMPORTS = 'too_many_imports';

/**
 * What a binding is: a request in, a result out. A thrown DuplexError crosses
 * the wire with its code, as it does from any handler.
 */
export type Invoke = (request: unknown, options?: { signal?: AbortSignal }) => Promise<unknown>;

/** Bounds on the bindings a scope holds. */
export interface LiveOptions {
  /** Bindings this side may have exported at once; the one past it is refused too_many_exports and registers nothing. Default: 1024. */
  maxExports?: number;
  /** Bindings this side may hold an attachment to at once. Default: 1024. */
  maxImports?: number;
}

/** What a scope holds now, which is what a scenario asserts is nothing once an exchange is over. */
export interface Counts {
  exports: number;
  imports: number;
}

interface ReferenceWire {
  binding: string;
  contract: string;
}

const MINTED: unique symbol = Symbol('nightseam.live.reference');

/**
 * Names one binding of one scope. It has no public constructor: it is minted by
 * export or by decode, carries the scope it was minted in, and is refused
 * reference_foreign anywhere else — which is how a token detached from its
 * connection cannot be imported again.
 */
export class Reference {
  /** @internal */ readonly [MINTED]: true = true;
  /** @internal */ readonly binding: string;
  /** @internal */ readonly scope: LiveScope | undefined;
  readonly contract: string;

  /** @internal */
  constructor(binding: string, contract: string, scope: LiveScope | undefined, minted: typeof MINTED) {
    if (minted !== MINTED) throw new DuplexError(CONTRACT_INVALID, 'A live reference is minted by a scope.');
    this.binding = binding;
    this.contract = contract;
    this.scope = scope;
  }

  /** The reference as it travels inside a payload: an ordinary value of the message carrying it. */
  toJSON(): ReferenceWire {
    return { binding: this.binding, contract: this.contract };
  }
}

interface Binding {
  contract: string;
  invoke: Invoke;
  released: boolean;
}

interface Attachment {
  contract: string;
  invoke: Invoke;
  released: boolean;
}

const OVER = new WeakMap<DuplexPeer, LiveScope>();

/**
 * Makes the live layer over a peer, registering its operations on it; a peer
 * carries one scope. Make it before the peer is given a connection, as a tunnel
 * is made: a peer already reading can refuse the other side's first live.invoke
 * method_not_found before the handler is there.
 */
export function liveOver(peer: DuplexPeer, options: LiveOptions = {}): LiveScope {
  return new LiveScope(peer, options);
}

/**
 * The scope over a peer, which is how generated code inside a handler reaches
 * the layer: the handler has the peer, and the peer has one scope or none.
 */
export function scopeOf(peer: DuplexPeer): LiveScope | undefined {
  return OVER.get(peer);
}

/**
 * Gives another scope a binding of its own over a callable this one imported.
 * It is composition, not a mechanism: the destination gets an ordinary export
 * whose function is the origin's, and so a lifetime of its own. Releasing the
 * forwarded binding does not release the origin; an invocation through a
 * released or ended origin fails with the origin's refusal, which is what the
 * destination's caller is told.
 */
export function forward(destination: LiveScope, contract: string, origin: Invoke): Reference {
  if (!origin) throw new DuplexError(CONTRACT_INVALID, "Forwarding needs the origin's function.");
  return destination.export(contract, origin);
}

/** The live layer over one peer: what this side exported over that connection, what it imported, and nothing that outlives it. */
export class LiveScope {
  private readonly nonce: string;
  private readonly maxExports: number;
  private readonly maxImports: number;
  private readonly exports = new Map<string, Binding>();
  private readonly imports = new Map<string, Attachment>();
  private readonly gone = new Set<string>();
  private readonly order: string[] = [];
  private readonly inflight = new Set<AbortController>();
  private next = 1;
  private closed = false;
  /** The peer the scope runs over. */
  readonly peer: DuplexPeer;

  constructor(peer: DuplexPeer, options: LiveOptions = {}) {
    if (!peer) throw new DuplexError(CONTRACT_INVALID, 'A live scope needs a peer.');
    this.peer = peer;
    this.maxExports = bound(options.maxExports, 'maxExports');
    this.maxImports = bound(options.maxImports, 'maxImports');
    this.nonce = nonce();
    peer.handle(INVOKE_METHOD, (params, context) => this.onInvoke(params, context.signal));
    peer.onEvent(RELEASE_EVENT, (data) => this.onRelease(data));
    // A reference does not survive its connection and reconnection revives
    // nothing: the next connection is another scope, whose nonce is another.
    peer.onClose(() => this.end());
    OVER.set(peer, this);
  }

  /** What the scope holds now. */
  counts(): Counts {
    return { exports: this.exports.size, imports: this.imports.size };
  }

  /**
   * Makes a binding of a native function and returns the reference that names
   * it. Exporting the same function twice makes two bindings: native identity
   * is nobody's guarantee across a wire, and two bindings are two lifetimes,
   * which is what separate release needs.
   */
  export(contract: string, invoke: Invoke): Reference {
    if (!contract) throw this.refuse(contract, CONTRACT_INVALID, 'A binding is exported for a contract.');
    if (typeof invoke !== 'function') throw this.refuse(contract, CONTRACT_INVALID, 'A binding is a function.');
    if (this.closed) throw this.refuse(contract, SCOPE_CLOSED, 'The scope ended.');
    if (this.exports.size >= this.maxExports)
      throw this.refuse(contract, TOO_MANY_EXPORTS, 'No room for another exported binding.');
    const id = `${this.nonce}.${this.next++}`;
    this.exports.set(id, { contract, invoke, released: false });
    this.observe({ type: 'live.exported', at: new Date(), contract, binding: id });
    return new Reference(id, contract, this, MINTED);
  }

  /**
   * Reads a reference out of a payload of this scope. It is the only way a
   * reference enters the language besides an export, and it is why a detached
   * token cannot be imported again: what it mints is bound to this scope, and
   * this scope's bindings are the ones its nonce names.
   */
  decode(raw: unknown): Reference {
    const wire = raw as Partial<ReferenceWire> | null;
    if (!wire || typeof wire !== 'object' || typeof wire.binding !== 'string' || typeof wire.contract !== 'string') {
      throw new DuplexError(CONTRACT_INVALID, 'A live reference is a binding and a contract.');
    }
    if (!wire.binding || !wire.contract) {
      throw new DuplexError(CONTRACT_INVALID, 'A live reference is a binding and a contract.');
    }
    return new Reference(wire.binding, wire.contract, this, MINTED);
  }

  /**
   * The attachment to a binding, as a function to call it with. The same
   * binding imported again gives the same attachment: one binding is one
   * dispatch, and two imports never become two readers competing for one reply.
   * A reference this side exported resolves to the function behind it, with
   * nothing crossing the wire.
   */
  import(reference: Reference, contract: string): Invoke {
    if (!contract) throw this.refuse(contract, CONTRACT_INVALID, 'A binding is imported for a contract.');
    if (!(reference instanceof Reference) || reference.scope !== this)
      throw this.refuse(contract, REFERENCE_FOREIGN, 'The reference was minted in another scope.');
    if (reference.contract !== contract)
      throw this.refuse(
        contract,
        CONTRACT_MISMATCH,
        `The reference carries ${reference.contract} where ${contract} is expected.`,
      );
    if (this.closed) throw this.refuse(contract, SCOPE_CLOSED, 'The scope ended.');
    if (this.gone.has(reference.binding)) throw this.refuse(contract, REFERENCE_RELEASED, 'The binding was released.');

    const own = this.exports.get(reference.binding);
    if (own) {
      if (own.contract !== contract)
        throw this.refuse(contract, CONTRACT_MISMATCH, `The binding carries ${own.contract}.`);
      return this.local(own);
    }
    const held = this.imports.get(reference.binding);
    if (held) {
      if (held.contract !== contract)
        throw this.refuse(contract, CONTRACT_MISMATCH, `The binding is attached as ${held.contract}.`);
      return held.invoke;
    }
    if (this.imports.size >= this.maxImports)
      throw this.refuse(contract, TOO_MANY_IMPORTS, 'No room for another imported binding.');
    const fresh: Attachment = { contract, invoke: async () => undefined, released: false };
    fresh.invoke = this.remote(reference.binding, fresh);
    this.imports.set(reference.binding, fresh);
    this.observe({ type: 'live.imported', at: new Date(), contract, binding: reference.binding });
    return fresh.invoke;
  }

  /**
   * Invalidates a binding and every alias of it, idempotently, whether this
   * side exported it or imported it, and tells the other side. It is a barrier:
   * the next invocation is refused reference_released and the ones already
   * dispatched settle and are delivered. It releases nothing else, and it is
   * not a cancellation — neither of an invocation in flight nor of whatever the
   * application does behind the callable.
   */
  release(reference: Reference): void {
    if (!(reference instanceof Reference) || reference.scope !== this)
      throw this.refuse(reference?.contract ?? '', REFERENCE_FOREIGN, 'The reference was minted in another scope.');
    this.releaseBinding(reference.binding, true);
  }

  /**
   * Ends the scope without ending the peer: every binding is invalidated and
   * every invocation this side has in flight is settled. It is not a barrier —
   * a barrier is what release is — and it does not roll back an effect an
   * invocation already had.
   */
  close(): void {
    this.end();
  }

  private end(): void {
    if (this.closed) return;
    this.closed = true;
    if (OVER.get(this.peer) === this) OVER.delete(this.peer);
    const pending = [...this.inflight];
    this.inflight.clear();
    this.exports.clear();
    this.imports.clear();
    for (const controller of pending) controller.abort();
  }

  /**
   * A reference of this side's own making, handed back and imported here. It
   * reaches the function behind the binding without a frame: the alternative is
   * a peer speaking to itself down its own connection.
   */
  private local(own: Binding): Invoke {
    return async (request, options) => {
      if (this.closed) throw new DuplexError(SCOPE_CLOSED, 'The scope ended.');
      if (own.released) throw new DuplexError(REFERENCE_RELEASED, 'The binding was released.');
      return this.invokeScoped(own.invoke, request, options?.signal);
    };
  }

  /**
   * An attachment to the other side's binding. Release is a barrier here: it
   * refuses the next invocation and leaves the ones already sent to settle,
   * since releasing a reference says nothing about work already asked for.
   */
  private remote(id: string, held: Attachment): Invoke {
    return async (request, options) => {
      if (this.closed) throw new DuplexError(SCOPE_CLOSED, 'The scope ended.');
      if (held.released) throw new DuplexError(REFERENCE_RELEASED, 'The binding was released.');
      const controller = new AbortController();
      const abort = () => controller.abort();
      options?.signal?.addEventListener('abort', abort, { once: true });
      if (options?.signal?.aborted) controller.abort();
      this.inflight.add(controller);
      try {
        return await this.peer.call(
          INVOKE_METHOD,
          { binding: id, contract: held.contract, request },
          {
            signal: controller.signal,
          },
        );
      } catch (error) {
        if (this.closed && !options?.signal?.aborted) throw new DuplexError(SCOPE_CLOSED, 'The scope ended.');
        throw error;
      } finally {
        this.inflight.delete(controller);
        options?.signal?.removeEventListener('abort', abort);
      }
    };
  }

  /**
   * Answers the other side's invocation of a binding of ours. It proves the
   * binding is one this scope exported and that the contract named is the one
   * it was exported for, and then it is the function's business.
   */
  private async onInvoke(params: unknown, signal: AbortSignal): Promise<unknown> {
    const named = params as Partial<{ binding: string; contract: string; request: unknown }> | null;
    if (
      !named ||
      typeof named !== 'object' ||
      typeof named.binding !== 'string' ||
      typeof named.contract !== 'string'
    ) {
      throw new DuplexError(CONTRACT_INVALID, 'live.invoke names a binding and a contract.');
    }
    if (this.closed) throw this.refuse(named.contract, SCOPE_CLOSED, 'The scope ended.');
    const own = this.exports.get(named.binding);
    if (!own) throw this.refuse(named.contract, REFERENCE_UNKNOWN, `No binding ${named.binding} in this scope.`);
    if (own.contract !== named.contract)
      throw this.refuse(named.contract, CONTRACT_MISMATCH, `The binding carries ${own.contract}.`);
    if (own.released) throw this.refuse(own.contract, REFERENCE_RELEASED, 'The binding was released.');
    const result = await this.invokeScoped(own.invoke, named.request, signal);
    return result === undefined ? null : result;
  }

  /** Settle callers on closure even when their implementation ignores its signal. */
  private async invokeScoped(invoke: Invoke, request: unknown, signal?: AbortSignal): Promise<unknown> {
    if (this.closed) throw new DuplexError(SCOPE_CLOSED, 'The scope ended.');
    const controller = new AbortController();
    const abort = () => controller.abort();
    signal?.addEventListener('abort', abort, { once: true });
    this.inflight.add(controller);
    let stopped!: () => void;
    const ended = new Promise<never>((_, reject) => {
      stopped = () =>
        reject(
          this.closed && !signal?.aborted
            ? new DuplexError(SCOPE_CLOSED, 'The scope ended.')
            : new DuplexError('cancelled', 'The invocation was cancelled.'),
        );
      controller.signal.addEventListener('abort', stopped, { once: true });
    });
    if (signal?.aborted) controller.abort();
    try {
      const result = Promise.resolve().then(() => {
        if (controller.signal.aborted) return ended;
        return invoke(request, { signal: controller.signal });
      });
      return await Promise.race([result, ended]);
    } finally {
      this.inflight.delete(controller);
      signal?.removeEventListener('abort', abort);
      controller.signal.removeEventListener('abort', stopped);
    }
  }

  /**
   * Takes the other side's word that it is done with a binding, or that one it
   * exported is gone. It releases here and tells nobody: the side that released
   * already knows.
   */
  private onRelease(data: unknown): void {
    const named = data as Partial<{ binding: string }> | null;
    if (!named || typeof named !== 'object' || typeof named.binding !== 'string' || !named.binding) return;
    this.releaseBinding(named.binding, false);
  }

  private releaseBinding(id: string, tell: boolean): void {
    if (this.closed) return;
    let contract = '';
    let found = false;
    const own = this.exports.get(id);
    if (own) {
      own.released = true;
      contract = own.contract;
      found = true;
      this.exports.delete(id);
    }
    const held = this.imports.get(id);
    if (held) {
      held.released = true;
      contract = held.contract;
      found = true;
      this.imports.delete(id);
    }
    const tombstoned = this.gone.has(id);
    if (tombstoned && !found) return;
    // What a released binding leaves behind is one id, so that an alias
    // imported again is refused for the reason it was actually refused for.
    // The set is bounded by the tables it shadows and forgets its oldest past
    // that: a binding whose tombstone is gone is refused reference_unknown by
    // the side that exported it, which is a refusal either way.
    if (!tombstoned) {
      this.gone.add(id);
      this.order.push(id);
      while (this.order.length > this.maxExports + this.maxImports) {
        this.gone.delete(this.order.shift()!);
      }
    }
    if (found) this.observe({ type: 'live.released', at: new Date(), contract, binding: id });
    if (tell) void this.peer.emit(RELEASE_EVENT, { binding: id }).catch(() => {});
  }

  private refuse(contract: string, code: string, reason: string): DuplexError {
    this.observe({ type: 'live.refused', at: new Date(), contract, code, reason });
    return new DuplexError(code, reason);
  }

  private observe(event: Parameters<DuplexPeer['observe']>[0]): void {
    this.peer.observe(event);
  }
}

function bound(value: number | undefined, name: string): number {
  if (value === undefined) return 1024;
  if (!Number.isInteger(value) || value <= 0) {
    throw new DuplexError(CONTRACT_INVALID, `${name} must be a positive integer.`);
  }
  return value;
}

function nonce(): string {
  const bytes = new Uint8Array(8);
  crypto.getRandomValues(bytes);
  return [...bytes].map((b) => b.toString(16).padStart(2, '0')).join('');
}
