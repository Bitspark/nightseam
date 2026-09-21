/// <reference lib="esnext.disposable" preserve="true" />
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
 * carries it. Invocation looks up the id in the exporting scope; serialized
 * bytes from an ended connection do not resolve in a fresh scope. A native
 * Reference comes from export or decode and records that scope. Decode accepts
 * caller-supplied bytes; it does not prove how they arrived.
 *
 * The layer proves which binding of which contract, and never who may call it.
 */
import { DuplexError, UnpublishedError, type DuplexPeer } from '@nightseam/runtime';
export { valueEnvironment } from './value_adapter.ts';

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
  digest?: string;
}

const MINTED: unique symbol = Symbol('nightseam.live.reference');

/**
 * Names one binding of one scope. It has no public constructor: it is minted by
 * export or by decode, carries the scope it was minted in, and is refused
 * reference_foreign when that native object is imported into another scope.
 * Serialized data may be decoded again; invocation must still resolve its
 * binding id in the exporting scope.
 */
export class Reference {
  /** @internal */ readonly [MINTED]: true = true;
  /** @internal */ readonly binding: string;
  /** @internal */ readonly scope: LiveScope | undefined;
  readonly contract: string;
  readonly digest: string;

  /** @internal */
  constructor(binding: string, contract: string, digest: string, scope: LiveScope | undefined, minted: typeof MINTED) {
    if (minted !== MINTED) throw new DuplexError(CONTRACT_INVALID, 'A live reference is minted by a scope.');
    this.binding = binding;
    this.contract = contract;
    this.digest = digest;
    this.scope = scope;
  }

  /** The binding, contract and any digest as an ordinary payload value, without the native scope association. */
  toJSON(): ReferenceWire {
    return { binding: this.binding, contract: this.contract, ...(this.digest ? { digest: this.digest } : {}) };
  }
}

interface Binding {
  owner: OwnerState;
  contract: string;
  digest: string;
  invoke: Invoke;
  released: boolean;
}

interface Attachment {
  owner: OwnerState;
  contract: string;
  digest: string;
  invoke: Invoke;
  released: boolean;
}

interface Allocation {
  id: string;
  imported: boolean;
  entry: Binding | Attachment;
}

interface ValueBatch {
  parent?: ValueBatch;
  active: boolean;
  allocations: Allocation[];
}

interface OwnerState {
  scope: ScopeState;
  parent?: OwnerState;
  children?: Set<OwnerState>;
  owned?: Map<string, boolean>;
  released: boolean;
  view?: LiveOwner;
}

interface ReleaseNotice {
  id: string;
  contract: string;
  found: boolean;
  tell: boolean;
}

interface ScopeState {
  rootOwner?: OwnerState;
  exportBinding(
    owner: OwnerState,
    batch: ValueBatch | undefined,
    contract: string,
    digest: string,
    invoke: Invoke,
  ): Reference;
  importBinding(
    owner: OwnerState,
    batch: ValueBatch | undefined,
    reference: Reference,
    contract: string,
    digest: string,
  ): Invoke;
  releaseBindings(bindings: { id: string; tell: boolean }[]): void;
  refuse(contract: string, code: string, reason: string): DuplexError;
  root: LiveScope;
  peer: DuplexPeer;
  nonce: string;
  maxExports: number;
  maxImports: number;
  exports: Map<string, Binding>;
  imports: Map<string, Attachment>;
  gone: Set<string>;
  order: string[];
  inflight: Set<AbortController>;
  next: number;
  closed: boolean;
}

const OVER = new WeakMap<DuplexPeer, LiveScope>();

let ownerView: (state: OwnerState, batch?: ValueBatch) => LiveOwner;

/** A caller-chosen lifetime: owns new bindings and borrows existing aliases. */
export class LiveOwner {
  private state: OwnerState;
  private batch?: ValueBatch;

  private constructor(state: OwnerState, batch?: ValueBatch) {
    this.state = state;
    this.batch = batch;
  }

  static {
    ownerView = (state, batch) => new LiveOwner(state, batch);
  }

  /** The connection scope this lifetime belongs to. */
  get scope(): LiveScope {
    return this.state.scope.root;
  }

  /** A nested lifetime; releasing this owner also releases its descendants. */
  child(): LiveOwner {
    return ownerView({ scope: this.state.scope, parent: this.state, released: false }, this.batch);
  }

  /** Direct allocations only, excluding children and borrowed attachments. */
  counts(): Counts {
    const counts = { exports: 0, imports: 0 };
    for (const imported of this.state.owned?.values() ?? []) {
      if (imported) counts.imports++;
      else counts.exports++;
    }
    return counts;
  }

  /** Revokes owned bindings, children first, once, leaving the scope open. */
  release(): void {
    this.state.scope.releaseBindings(endOwner(this.state).map((id) => ({ id, tell: true })));
  }

  [Symbol.dispose](): void {
    this.release();
  }

  /** Creates a binding owned by this lifetime. An empty digest leaves the declaration revision unspecified. */
  export(contract: string, digest: string, invoke: Invoke): Reference {
    return this.state.scope.exportBinding(this.state, this.batch, contract, digest, invoke);
  }

  /** Owns a fresh attachment or borrows an existing one; two specified digests must agree before either. */
  import(reference: Reference, contract: string, digest: string): Invoke {
    return this.state.scope.importBinding(this.state, this.batch, reference, contract, digest);
  }

  /**
   * Builds an unpublished JSON snapshot synchronously using the supplied owner
   * view. Failure discards only newly allocated bindings, without publishing a
   * release for unpublished exports. Nested successful builds join their parent;
   * successful outer builds commit to the owner, irrespective of later RPC errors.
   */
  exportValue(build: (owner: LiveOwner) => unknown): unknown {
    return this.build((owner) => snapshot(build(owner)));
  }

  /**
   * Builds a complete payload, then publishes it. Only proof of local rejection
   * before queuing unwinds these allocations; uncertain outcomes retain them
   * until owner release or scope end. Captured build views are inactive before
   * publication, so subsequent conversions form independent batches.
   */
  async publishValue<T>(build: (owner: LiveOwner) => unknown, publish: (payload: unknown) => Promise<T>): Promise<T> {
    let allocations: Allocation[] = [];
    const value = this.build(
      (owner) => snapshot(build(owner)),
      (completed) => {
        allocations = completed;
      },
    );
    try {
      return await publish(value);
    } catch (error) {
      if (error instanceof UnpublishedError) this.discard(allocations);
      throw error;
    }
  }

  /** A failed synchronous import walk releases new attachments, never its borrowed aliases. */
  importValue<T>(build: (owner: LiveOwner) => T): T {
    return this.build(build);
  }

  private build<T>(build: (owner: LiveOwner) => T, completed?: (allocations: Allocation[]) => void): T {
    if (this.state.scope.closed) throw this.state.scope.refuse('', SCOPE_CLOSED, 'The scope ended.');
    if (ownerEnded(this.state)) throw this.state.scope.refuse('', REFERENCE_RELEASED, 'The owner was released.');
    const batch: ValueBatch = { active: true, allocations: [] };
    if (this.batch?.active) batch.parent = this.batch;
    const view = ownerView(this.state, batch);
    let committed = false;
    try {
      const value = build(view);
      if (value instanceof Promise) throw new TypeError('A live conversion must finish synchronously.');
      committed = true;
      return value;
    } finally {
      const allocations = batch.allocations;
      if (committed && batch.parent?.active) batch.parent.allocations.push(...allocations);
      batch.active = false;
      batch.allocations = [];
      delete batch.parent;
      if (!committed) this.discard(allocations);
      else completed?.(allocations);
    }
  }

  private discard(allocations: Allocation[]): void {
    const scope = this.state.scope;
    // Explicit or remote release may already have ended an allocation.
    scope.releaseBindings(
      allocations
        .filter(({ id, imported, entry }) => (imported ? scope.imports.get(id) : scope.exports.get(id)) === entry)
        .map(({ id, imported }) => ({ id, tell: imported })),
    );
  }
}

function snapshot(value: unknown): unknown {
  if (value instanceof Promise) throw new TypeError('A live export must finish synchronously.');
  const encoded = JSON.stringify(value);
  return encoded === undefined ? undefined : JSON.parse(encoded);
}

function ownerEnded(owner: OwnerState): boolean {
  for (let current: OwnerState | undefined = owner; current; current = current.parent) {
    if (current.released) return true;
  }
  return false;
}

function record(owner: OwnerState, batch: ValueBatch | undefined, id: string, imported: boolean): void {
  (owner.owned ??= new Map()).set(id, imported);
  for (let child = owner; child.parent; child = child.parent) {
    (child.parent.children ??= new Set()).add(child);
  }
  if (batch?.active) {
    const entry = (imported ? owner.scope.imports.get(id) : owner.scope.exports.get(id))!;
    batch.allocations.push({ id, imported, entry });
  }
}

function forget(owner: OwnerState, id: string): void {
  owner.owned?.delete(id);
  prune(owner);
}

function prune(owner: OwnerState): void {
  for (let child = owner; child.parent && !child.owned?.size && !child.children?.size; child = child.parent) {
    child.parent.children?.delete(child);
  }
}

function endOwner(owner: OwnerState): string[] {
  if (owner.released) return [];
  owner.released = true;
  const ids: string[] = [];
  for (const child of owner.children ?? []) ids.push(...endOwner(child));
  ids.push(...(owner.owned?.keys() ?? []));
  delete owner.owned;
  delete owner.children;
  prune(owner);
  return ids;
}

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
export function forward(destination: LiveOwner, contract: string, digest: string, origin: Invoke): Reference {
  if (!origin) throw new DuplexError(CONTRACT_INVALID, "Forwarding needs the origin's function.");
  return destination.export(contract, digest, origin);
}

const DIGEST = /^[0-9a-f]{64}$/;

function validDigest(digest: string): boolean {
  return typeof digest === 'string' && (digest === '' || DIGEST.test(digest));
}

function digestMismatch(left: string, right: string): boolean {
  return left !== '' && right !== '' && left !== right;
}

/** The live layer over one peer: what this side exported over that connection, what it imported, and nothing that outlives it. */
export class LiveScope {
  private state: ScopeState;
  private get nonce() {
    return this.state.nonce;
  }
  private get maxExports() {
    return this.state.maxExports;
  }
  private get maxImports() {
    return this.state.maxImports;
  }
  private get exports() {
    return this.state.exports;
  }
  private get imports() {
    return this.state.imports;
  }
  private get gone() {
    return this.state.gone;
  }
  private get order() {
    return this.state.order;
  }
  private get inflight() {
    return this.state.inflight;
  }
  private get next() {
    return this.state.next;
  }
  private set next(value: number) {
    this.state.next = value;
  }
  private get closed() {
    return this.state.closed;
  }
  private set closed(value: boolean) {
    this.state.closed = value;
  }
  /** The peer the scope runs over. */
  get peer(): DuplexPeer {
    return this.state.peer;
  }

  constructor(peer: DuplexPeer, options: LiveOptions = {}) {
    if (!peer) throw new DuplexError(CONTRACT_INVALID, 'A live scope needs a peer.');
    this.state = {
      root: this,
      exportBinding: (owner, batch, contract, digest, invoke) =>
        this.exportBinding(owner, batch, contract, digest, invoke),
      importBinding: (owner, batch, reference, contract, digest) =>
        this.importBinding(owner, batch, reference, contract, digest),
      releaseBindings: (bindings) => this.releaseBindings(bindings),
      refuse: (contract, code, reason) => this.refuse(contract, code, reason),
      peer,
      nonce: nonce(),
      maxExports: bound(options.maxExports, 'maxExports'),
      maxImports: bound(options.maxImports, 'maxImports'),
      exports: new Map(),
      imports: new Map(),
      gone: new Set(),
      order: [],
      inflight: new Set(),
      next: 1,
      closed: false,
    };
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

  /** The current root lifetime; release ends it and the next call creates a fresh one. */
  owner(): LiveOwner {
    if (!this.state.rootOwner || this.state.rootOwner.released) {
      this.state.rootOwner = { scope: this.state, released: false };
    }
    return (this.state.rootOwner.view ??= ownerView(this.state.rootOwner));
  }

  /**
   * Makes a binding of a native function and returns the reference that names
   * it. Exporting the same function twice makes two bindings: native identity
   * is nobody's guarantee across a wire, and two bindings are two lifetimes,
   * which is what separate release needs.
   */
  private exportBinding(
    owner: OwnerState,
    batch: ValueBatch | undefined,
    contract: string,
    digest: string,
    invoke: Invoke,
  ): Reference {
    if (!contract) throw this.refuse(contract, CONTRACT_INVALID, 'A binding is exported for a contract.');
    if (!validDigest(digest))
      throw this.refuse(contract, CONTRACT_INVALID, 'A declaration digest is lower-case SHA-256 hex.');
    if (typeof invoke !== 'function') throw this.refuse(contract, CONTRACT_INVALID, 'A binding is a function.');
    if (this.closed) throw this.refuse(contract, SCOPE_CLOSED, 'The scope ended.');
    if (ownerEnded(owner)) throw this.refuse(contract, REFERENCE_RELEASED, 'The owner was released.');
    if (this.exports.size >= this.maxExports)
      throw this.refuse(contract, TOO_MANY_EXPORTS, 'No room for another exported binding.');
    const id = `${this.nonce}.${this.next++}`;
    this.exports.set(id, { contract, digest, invoke, released: false, owner });
    record(owner, batch, id, false);
    this.observe({ type: 'live.exported', at: new Date(), contract, binding: id });
    return new Reference(id, contract, digest, this.state.root, MINTED);
  }

  /**
   * Reads a caller-supplied binding, contract and optional digest into a native reference of
   * this scope. It checks shape and associates the receiving scope; it proves
   * neither inbound-message provenance nor binding existence or authorization.
   * Import checks the expected contract; invocation resolves the binding.
   */
  decode(raw: unknown): Reference {
    const wire = raw as Partial<ReferenceWire> | null;
    if (!wire || typeof wire !== 'object' || typeof wire.binding !== 'string' || typeof wire.contract !== 'string') {
      throw new DuplexError(CONTRACT_INVALID, 'A live reference is a binding and a contract.');
    }
    if (!wire.binding || !wire.contract) {
      throw new DuplexError(CONTRACT_INVALID, 'A live reference is a binding and a contract.');
    }
    if ('digest' in wire && (typeof wire.digest !== 'string' || !DIGEST.test(wire.digest))) {
      throw new DuplexError(CONTRACT_INVALID, 'A declaration digest is lower-case SHA-256 hex.');
    }
    return new Reference(wire.binding, wire.contract, wire.digest ?? '', this.state.root, MINTED);
  }

  /**
   * The attachment to a binding, as a function to call it with. The same
   * binding imported again gives the same attachment: one binding is one
   * dispatch, and two imports never become two readers competing for one reply.
   * A reference this side exported resolves to the function behind it, with
   * nothing crossing the wire. Attaching to a remote binding does not prove
   * that it exists; invocation performs that lookup in the exporting scope.
   */
  private importBinding(
    owner: OwnerState,
    batch: ValueBatch | undefined,
    reference: Reference,
    contract: string,
    digest: string,
  ): Invoke {
    if (!contract) throw this.refuse(contract, CONTRACT_INVALID, 'A binding is imported for a contract.');
    if (!validDigest(digest))
      throw this.refuse(contract, CONTRACT_INVALID, 'A declaration digest is lower-case SHA-256 hex.');
    if (!(reference instanceof Reference) || reference.scope !== this.state.root)
      throw this.refuse(contract, REFERENCE_FOREIGN, 'The reference was minted in another scope.');
    if (reference.contract !== contract)
      throw this.refuse(
        contract,
        CONTRACT_MISMATCH,
        `The reference carries ${reference.contract} where ${contract} is expected.`,
      );
    if (digestMismatch(reference.digest, digest))
      throw this.refuse(
        contract,
        CONTRACT_MISMATCH,
        `The reference carries a different declaration digest for ${contract}.`,
      );
    if (this.closed) throw this.refuse(contract, SCOPE_CLOSED, 'The scope ended.');
    if (ownerEnded(owner)) throw this.refuse(contract, REFERENCE_RELEASED, 'The owner was released.');
    if (this.gone.has(reference.binding)) throw this.refuse(contract, REFERENCE_RELEASED, 'The binding was released.');

    const own = this.exports.get(reference.binding);
    if (own) {
      if (own.contract !== contract)
        throw this.refuse(contract, CONTRACT_MISMATCH, `The binding carries ${own.contract}.`);
      if (digestMismatch(own.digest, reference.digest) || digestMismatch(own.digest, digest))
        throw this.refuse(
          contract,
          CONTRACT_MISMATCH,
          `The binding carries a different declaration digest for ${contract}.`,
        );
      return this.local(own);
    }
    const held = this.imports.get(reference.binding);
    if (held) {
      if (held.contract !== contract)
        throw this.refuse(contract, CONTRACT_MISMATCH, `The binding is attached as ${held.contract}.`);
      if (digestMismatch(held.digest, reference.digest) || digestMismatch(held.digest, digest))
        throw this.refuse(
          contract,
          CONTRACT_MISMATCH,
          `The binding is attached with a different declaration digest for ${contract}.`,
        );
      if (!held.digest) held.digest = reference.digest || digest;
      return held.invoke;
    }
    if (this.imports.size >= this.maxImports)
      throw this.refuse(contract, TOO_MANY_IMPORTS, 'No room for another imported binding.');
    const fresh: Attachment = {
      contract,
      digest: reference.digest || digest,
      invoke: async () => undefined,
      released: false,
      owner,
    };
    fresh.invoke = this.remote(reference.binding, fresh);
    this.imports.set(reference.binding, fresh);
    record(owner, batch, reference.binding, true);
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
    if (!(reference instanceof Reference) || reference.scope !== this.state.root)
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
    if (OVER.get(this.peer) === this.state.root) OVER.delete(this.peer);
    const pending = [...this.inflight];
    this.inflight.clear();
    if (this.state.rootOwner) endOwner(this.state.rootOwner);
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
      if (this.closed) throw new UnpublishedError(new DuplexError(SCOPE_CLOSED, 'The scope ended.'));
      if (own.released) throw new UnpublishedError(new DuplexError(REFERENCE_RELEASED, 'The binding was released.'));
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
      if (this.closed) throw new UnpublishedError(new DuplexError(SCOPE_CLOSED, 'The scope ended.'));
      if (held.released) throw new UnpublishedError(new DuplexError(REFERENCE_RELEASED, 'The binding was released.'));
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
    if (this.closed) throw new UnpublishedError(new DuplexError(SCOPE_CLOSED, 'The scope ended.'));
    if (signal?.aborted) throw new UnpublishedError(new DuplexError('cancelled', 'The invocation was cancelled.'));
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
    } catch (error) {
      // A nested call's local refusal cannot prove this dispatched invocation
      // was unsent. Preserve its public information without forwarding proof.
      if (error instanceof UnpublishedError) {
        const dispatched = new DuplexError(error.code, error.message, error.data);
        Object.defineProperty(dispatched, 'cause', { value: error });
        throw dispatched;
      }
      throw error;
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
    this.releaseBindings([{ id, tell }]);
  }

  private releaseBindings(bindings: { id: string; tell: boolean }[]): void {
    // Revoke the whole batch before observers can reenter and invoke an alias.
    const notices = bindings.map(({ id, tell }) => this.takeRelease(id, tell));
    for (const notice of notices) {
      if (!notice) continue;
      const { id, contract, found, tell } = notice;
      if (found) this.observe({ type: 'live.released', at: new Date(), contract, binding: id });
      if (tell) void this.peer.emit(RELEASE_EVENT, { binding: id }).catch(() => {});
    }
  }

  private takeRelease(id: string, tell: boolean): ReleaseNotice | undefined {
    if (this.closed) return;
    let contract = '';
    let found = false;
    const own = this.exports.get(id);
    if (own) {
      own.released = true;
      contract = own.contract;
      found = true;
      this.exports.delete(id);
      forget(own.owner, id);
    }
    const held = this.imports.get(id);
    if (held) {
      held.released = true;
      contract = held.contract;
      found = true;
      this.imports.delete(id);
      forget(held.owner, id);
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
    return { id, contract, found, tell };
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
