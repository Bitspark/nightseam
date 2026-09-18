import { webSocketConnection } from '@nightseam/duplex';
import type { Frame, FrameConnection, WebSocketLike } from '@nightseam/duplex';
import { defaultPropagator, traceOf, traced } from './trace.ts';
import type { Propagator, Trace } from './trace.ts';
import type { Observer, ObserverEvent } from './observer.ts';

export type { WebSocketLike } from '@nightseam/duplex';

/** The endpoint selects this profile; it is not a WebSocket subprotocol token. */
export const DUPLEX_PROFILE = 'nightseam.duplex/1';
export const DUPLEX_DEFAULTS = Object.freeze({
  maxIncomingRequests: 64,
  maxPendingRequests: 128,
  maxQueuedMessages: 128,
  maxFrameBytes: 1_048_576,
  requestTimeoutMs: 30_000,
  writeTimeoutMs: 10_000,
  connectTimeoutMs: 30_000,
});

/** Public application errors may cross the wire; other handler errors are hidden. */
export class DuplexError extends Error {
  readonly code: string;
  readonly data?: unknown;

  constructor(code: string, message: string, data?: unknown) {
    super(message);
    this.name = 'DuplexError';
    this.code = code;
    this.data = data;
  }
}

export type PeerStatus = 'disconnected' | 'connecting' | 'connected';
/** A context makes the frame a child of the request the caller is serving. */
export interface CallOptions { signal?: AbortSignal; timeoutMs?: number; context?: RequestContext }
export interface EmitOptions { context?: RequestContext }
export interface RequestContext {
  signal: AbortSignal;
  peer: DuplexPeer;
  requestId: string;
  /** What the propagator read from the frame that started this request. */
  trace?: Trace;
}
export type RequestHandler = (params: unknown, context: RequestContext) => unknown | Promise<unknown>;
export type Dispatcher = (method: string, params: unknown, context: RequestContext) => unknown | Promise<unknown>;
export type EventListener = (event: string, data: unknown) => void | Promise<void>;
export interface PeerOptions {
  role?: 'client' | 'server';
  dispatch?: Dispatcher;
  webSocketFactory?: (url: string) => WebSocketLike;
  maxIncomingRequests?: number;
  maxPendingRequests?: number;
  maxQueuedMessages?: number;
  maxFrameBytes?: number;
  requestTimeoutMs?: number;
  writeTimeoutMs?: number;
  connectTimeoutMs?: number;
  /** Absent, the default mints W3C ids; an adapter for a tracing library replaces it. */
  propagator?: Propagator;
  onError?: (error: DuplexError) => void;
  observer?: Observer;
  /** Method or event name to family label; the generated install fills it. */
  families?: Record<string, string>;
}

type Timer = ReturnType<typeof setTimeout>;
/** How a request ended, as the observer's event spells it. */
type Outcome = Extract<ObserverEvent, { type: 'request.ended' }>['outcome'];
/** A decoded JSON envelope; the connection beneath carries it as a text frame. */
type Envelope = Record<string, unknown>;
interface Pending {
  resolve: (value: unknown) => void;
  reject: (error: DuplexError) => void;
  /** What an observer is told the call was, wherever and whenever it ends. */
  method: string;
  started: number;
  trace?: Trace;
  timer?: Timer;
  removeAbort?: () => void;
}
interface Incoming {
  controller: AbortController;
  timer: Timer;
  responded: boolean;
  /** The request's trace; its response, and nothing else, carries it back. */
  trace?: Trace;
  method: string;
  started: number;
}
interface Outgoing {
  text: string;
  started: number;
  sent: boolean;
  waited: boolean;
  resolve: () => void;
  reject: (error: DuplexError) => void;
}
interface QueuedEvent { name: string; data: unknown; bytes: number; trace?: Trace }

/**
 * A bounded full-duplex peer. Message routing never awaits application handlers.
 * Calls are not retried, and connections are never reopened automatically.
 * The caller owns endpoint authentication and authorization of incoming methods.
 */
export class DuplexPeer {
  private readonly options: PeerOptions;
  private readonly limits: typeof DUPLEX_DEFAULTS;
  private readonly propagator: Propagator;
  private readonly observer?: Observer;
  private readonly localPrefix: string;
  private readonly remotePrefix: string;
  private connection?: FrameConnection;
  private detach?: () => void;
  private state: PeerStatus = 'disconnected';
  private nextID = 0;
  private opening?: { resolve: () => void; reject: (error: DuplexError) => void; timer: Timer };
  private readonly pending = new Map<string, Pending>();
  private readonly incoming = new Map<string, Incoming>();
  private readonly handlers = new Map<string, RequestHandler>();
  private readonly listeners = new Set<EventListener>();
  private readonly closedListeners = new Set<(error: DuplexError) => void>();
  private readonly outgoing: Outgoing[] = [];
  private writeTimer?: Timer;
  private readonly events: QueuedEvent[] = [];
  private eventActive = false;
  private eventTimer?: Timer;
  private generation = 0;

  constructor(options: PeerOptions = {}) {
    this.options = options;
    if (options.role !== undefined && options.role !== 'client' && options.role !== 'server') {
      throw new DuplexError('invalid_options', 'Peer role must be client or server.');
    }
    this.limits = { ...DUPLEX_DEFAULTS };
    for (const key of Object.keys(DUPLEX_DEFAULTS) as (keyof typeof DUPLEX_DEFAULTS)[]) {
      const value = options[key];
      if (value !== undefined) {
        positiveInteger(value, key);
        // Values deliberately remain configurable without introducing unbounded queues.
        (this.limits as Record<string, number>)[key] = value;
      }
    }
    this.propagator = options.propagator ?? defaultPropagator;
    this.observer = options.observer;
    this.localPrefix = options.role === 'server' ? 's:' : 'c:';
    this.remotePrefix = options.role === 'server' ? 'c:' : 's:';
  }

  get status(): PeerStatus { return this.state; }

  /** The side of the connection this peer is; a tunnel over it chooses channel ids by it. */
  get role(): 'client' | 'server' { return this.options.role ?? 'client'; }

  /** Absolute ws/wss URLs are required. Factories may supply platform-specific auth. */
  connect(url: string): Promise<void> {
    if (this.connection) return Promise.reject(new DuplexError('already_connected', 'Peer already has a connection.'));
    let endpoint: URL;
    try { endpoint = new URL(url); } catch {
      return Promise.reject(new DuplexError('invalid_url', 'An absolute WebSocket URL is required.'));
    }
    if (!['ws:', 'wss:'].includes(endpoint.protocol) || endpoint.hash || endpoint.username || endpoint.password) {
      return Promise.reject(new DuplexError('invalid_url', 'Use an absolute ws/wss URL without credentials or a fragment.'));
    }
    let socket: WebSocketLike;
    try {
      socket = this.options.webSocketFactory?.(endpoint.href) ?? new WebSocket(endpoint.href);
    } catch {
      return Promise.reject(new DuplexError('connection_failed', 'Unable to create WebSocket.'));
    }
    return this.attach(socket);
  }

  /**
   * Attach an externally authenticated, connecting or open connection. A
   * WebSocket is wrapped by the adapter; the peer itself never touches one.
   */
  attach(connection: FrameConnection | WebSocketLike): Promise<void> {
    if (this.connection) return Promise.reject(new DuplexError('already_connected', 'Peer already has a connection.'));
    const frames = isWebSocketLike(connection) ? webSocketConnection(connection) : connection;
    if (frames.state !== 'connecting' && frames.state !== 'open') {
      return Promise.reject(new DuplexError('disconnected', 'Cannot attach a closing or closed WebSocket.'));
    }
    this.connection = frames;
    this.generation++;
    this.state = frames.state === 'open' ? 'connected' : 'connecting';
    if (this.observer && this.state === 'connected') this.observe({ type: 'connection.opened', at: new Date(), role: this.role });
    const current = () => this.connection === frames;
    this.detach = frames.listen({
      open: () => {
        if (!current()) return;
        this.state = 'connected';
        if (this.observer) this.observe({ type: 'connection.opened', at: new Date(), role: this.role });
        if (this.opening) {
          clearTimeout(this.opening.timer);
          this.opening.resolve();
          this.opening = undefined;
        }
      },
      frame: frame => { if (current()) this.receive(frame); },
      close: (code, reason) => {
        if (current()) this.fail(new DuplexError('disconnected', 'Connection closed; outstanding call outcomes may be unknown.'), false, code, reason);
      },
      error: () => {
        if (current()) this.fail(new DuplexError('connection_failed', 'WebSocket connection failed.'));
      },
    });
    if (this.state === 'connected') return Promise.resolve();
    return new Promise<void>((resolve, reject) => {
      const timer = setTimeout(() => this.fail(new DuplexError('connect_timeout', 'Connection timed out.')), this.limits.connectTimeoutMs);
      this.opening = { resolve, reject, timer };
    });
  }

  close(): void { this.fail(new DuplexError('disconnected', 'Connection closed by caller.'), true, 1000); }

  onClose(listener: (error: DuplexError) => void): () => void {
    this.closedListeners.add(listener);
    return () => { this.closedListeners.delete(listener); };
  }

  handle(method: string, handler: RequestHandler): () => void {
    requireName(method, 'method');
    if (this.handlers.has(method)) throw new DuplexError('duplicate_handler', `Handler already registered for ${method}.`);
    this.handlers.set(method, handler);
    return () => { if (this.handlers.get(method) === handler) this.handlers.delete(method); };
  }

  onEvent(listener: EventListener): () => void;
  onEvent(event: string, listener: (data: unknown) => void | Promise<void>): () => void;
  onEvent(eventOrListener: string | EventListener, listener?: (data: unknown) => void | Promise<void>): () => void {
    let callback: EventListener;
    if (typeof eventOrListener === 'string') {
      requireName(eventOrListener, 'event');
      if (!listener) throw new DuplexError('invalid_listener', 'An event listener is required.');
      callback = (event, data) => { if (event === eventOrListener) return listener(data); };
    } else {
      callback = eventOrListener;
    }
    this.listeners.add(callback);
    return () => { this.listeners.delete(callback); };
  }

  call<T = unknown>(method: string, params: unknown = {}, options: CallOptions = {}): Promise<T> {
    try {
      requireName(method, 'method');
      if (options.timeoutMs !== undefined) positiveInteger(options.timeoutMs, 'timeoutMs');
    } catch (error) { return Promise.reject(error); }
    if (!this.isOpen()) return Promise.reject(new DuplexError('not_connected', 'Peer is not connected.'));
    if (options.signal?.aborted) return Promise.reject(new DuplexError('cancelled', 'Call was cancelled before sending.'));
    if (this.pending.size >= this.limits.maxPendingRequests) {
      return Promise.reject(new DuplexError('busy', 'Outstanding call limit reached.'));
    }
    if (this.nextID >= Number.MAX_SAFE_INTEGER) {
      return Promise.reject(new DuplexError('identifier_exhausted', 'Create a new peer before issuing further calls.'));
    }
    const id = this.localPrefix + (++this.nextID).toString(10);
    // One trace for the exchange: the request carries it and its cancel repeats it.
    const trace = this.propagator.inject(options.context);
    return new Promise<T>((resolve, reject) => {
      const pending: Pending = { resolve: value => resolve(value as T), reject, method, started: Date.now(), trace };
      this.pending.set(id, pending);
      const cancel = (error: DuplexError, outcome: Outcome) => {
        if (!this.takePending(id)) return;
        this.ended(id, pending, outcome, error.code);
        reject(error);
        void this.send(traced({ version: 1, kind: 'cancel', id }, trace), method).catch(() => {});
      };
      if (this.observer) this.observe({ type: 'request.started', at: new Date(), id, method, incoming: false, trace, family: this.family(method) });
      pending.timer = setTimeout(() => cancel(new DuplexError('request_timeout', `Call ${method} timed out; its outcome may be unknown.`), 'timeout'), options.timeoutMs ?? this.limits.requestTimeoutMs);
      if (options.signal) {
        const abort = () => cancel(new DuplexError('cancelled', 'Call was cancelled; its outcome may be unknown.'), 'cancelled');
        options.signal.addEventListener('abort', abort, { once: true });
        pending.removeAbort = () => options.signal!.removeEventListener('abort', abort);
      }
      void this.send(traced({ version: 1, kind: 'request', id, method, params }, trace), method).catch(failure => {
        const unsent = this.takePending(id);
        if (!unsent) return;
        const error = asError(failure, 'send_failed');
        this.ended(id, unsent, 'error', error.code);
        unsent.reject(error);
      });
    });
  }

  /** Resolves when accepted by the socket and its reported byte buffer drains. */
  emit(event: string, data: unknown = null, options: EmitOptions = {}): Promise<void> {
    try { requireName(event, 'event'); } catch (error) { return Promise.reject(error); }
    return this.send(traced({ version: 1, kind: 'event', event, data }, this.propagator.inject(options.context)), event);
  }

  private isOpen(): boolean { return this.state === 'connected' && this.connection?.state === 'open'; }

  private takePending(id: string): Pending | undefined {
    const pending = this.pending.get(id);
    if (!pending) return;
    this.pending.delete(id);
    clearTimeout(pending.timer);
    pending.removeAbort?.();
    return pending;
  }

  private send(envelope: Envelope, name = ''): Promise<void> {
    if (!this.isOpen()) return Promise.reject(new DuplexError('not_connected', 'Peer is not connected.'));
    let text: string;
    try {
      text = JSON.stringify(envelope, (_key, value: unknown) => {
        if (typeof value === 'function' || typeof value === 'symbol' || (typeof value === 'number' && !Number.isFinite(value))) {
          throw new Error('Not a JSON value.');
        }
        return value;
      });
    } catch {
      return Promise.reject(new DuplexError('invalid_message', 'Frame must contain serializable JSON values.'));
    }
    const bytes = new TextEncoder().encode(text).byteLength;
    if (bytes > this.limits.maxFrameBytes) {
      return Promise.reject(new DuplexError('frame_too_large', 'Outgoing frame exceeds the size limit.'));
    }
    if (this.outgoing.length >= this.limits.maxQueuedMessages) {
      const error = new DuplexError('busy', 'Output consumer is stalled; queue limit reached.');
      if (this.observer) this.pressure(this.outgoing.length, true);
      this.fail(error);
      return Promise.reject(error);
    }
    return new Promise<void>((resolve, reject) => {
      this.outgoing.push({ text, started: Date.now(), sent: false, waited: false, resolve, reject });
      if (this.observer) {
        // A frame the peer accepted for sending; what precedes this rejected it.
        const kind = envelope.kind as string;
        const trace = traceOf(envelope);
        const family = this.family(name);
        this.observe({ type: 'frame.sent', at: new Date(), kind, name, bytes, id: envelope.id as string | undefined, trace, family });
        if (kind === 'event') this.observe({ type: 'event.emitted', at: new Date(), name, bytes, trace, family });
      }
      this.flush();
    });
  }

  private flush(): void {
    if (this.writeTimer || !this.isOpen()) return;
    const connection = this.connection!;
    while (this.outgoing.length) {
      const item = this.outgoing[0];
      if (Date.now() - item.started >= this.limits.writeTimeoutMs) {
        if (this.observer) this.pressure(this.outgoing.length, true);
        this.fail(new DuplexError('write_timeout', 'Socket output did not drain before the write deadline.'));
        return;
      }
      if (!item.sent && connection.buffered === 0) {
        try { connection.send({ kind: 'text', data: item.text }); item.sent = true; } catch {
          this.fail(new DuplexError('send_failed', 'WebSocket send failed.'));
          return;
        }
      }
      if (item.sent && connection.buffered === 0) {
        this.outgoing.shift();
        item.resolve();
      } else {
        // One event for a frame that meets a full pipe, not one for every retry.
        if (!item.waited) {
          item.waited = true;
          if (this.observer) this.pressure(this.outgoing.length, false);
        }
        this.writeTimer = setTimeout(() => { this.writeTimer = undefined; this.flush(); }, 5);
        return;
      }
    }
  }

  private receive(incoming: Frame): void {
    if (!this.isOpen()) return;
    if (incoming.kind !== 'text') {
      this.fail(new DuplexError('invalid_message', 'Only JSON text frames are supported.'));
      return;
    }
    const data = incoming.data;
    const bytes = new TextEncoder().encode(data).byteLength;
    if (bytes > this.limits.maxFrameBytes) {
      this.fail(new DuplexError('frame_too_large', 'Incoming frame exceeds the size limit.'));
      return;
    }
    let frame: Envelope;
    try {
      frame = decodeEnvelope(data, this.localPrefix, this.remotePrefix);
    } catch {
      this.fail(new DuplexError('invalid_message', 'Invalid duplex frame.'));
      return;
    }
    const trace = traceOf(frame);
    if (this.observer) {
      const name = this.nameOf(frame);
      this.observe({ type: 'frame.received', at: new Date(), kind: frame.kind as string, name, bytes, id: frame.id as string | undefined, trace, family: this.family(name) });
    }
    switch (frame.kind) {
      case 'response': {
        const id = frame.id as string;
        const pending = this.takePending(id);
        if (!pending) return; // A cancellation or deadline may precede a late response.
        if (isObject(frame.error)) {
          const error = new DuplexError(frame.error.code as string, frame.error.message as string, frame.error.data);
          this.ended(id, pending, 'error', error.code);
          pending.reject(error);
        } else {
          this.ended(id, pending, 'ok');
          pending.resolve(frame.result);
        }
        break;
      }
      case 'cancel': {
        const incoming = this.incoming.get(frame.id as string);
        if (incoming) {
          incoming.controller.abort();
          this.respond(frame.id as string, incoming, undefined, new DuplexError('cancelled', 'Request was cancelled.'), 'cancelled');
        }
        break;
      }
      case 'request': this.request(frame.id as string, frame.method as string, frame.params, trace); break;
      case 'event': this.event(frame.event as string, frame.data, bytes, trace); break;
    }
  }

  private request(id: string, method: string, params: unknown, trace?: Trace): void {
    if (this.incoming.has(id)) {
      this.fail(new DuplexError('invalid_message', 'An incoming request ID is already active.'));
      return;
    }
    if (this.incoming.size >= this.limits.maxIncomingRequests) {
      void this.send(traced({ version: 1, kind: 'response', id, error: { code: 'busy', message: 'Incoming request limit reached.' } }, trace), method).catch(error => this.fail(asError(error)));
      return;
    }
    const controller = new AbortController();
    const incoming: Incoming = {
      controller,
      responded: false,
      trace,
      method,
      started: Date.now(),
      timer: setTimeout(() => {
        controller.abort();
        this.respond(id, incoming, undefined, new DuplexError('request_timeout', 'Request deadline exceeded.'), 'timeout');
      }, this.limits.requestTimeoutMs),
    };
    this.incoming.set(id, incoming);
    if (this.observer) this.observe({ type: 'request.started', at: new Date(), id, method, incoming: true, trace, family: this.family(method) });
    const context: RequestContext = { peer: this, signal: controller.signal, requestId: id };
    this.propagator.extract(context, trace);
    void Promise.resolve().then(() => {
      // The peer can close or cancel before the handler's first microtask.
      if (controller.signal.aborted) throw new DuplexError('cancelled', 'Request was cancelled.');
      const handler = this.handlers.get(method);
      if (handler) return handler(params, context);
      if (this.options.dispatch) return this.options.dispatch(method, params, context);
      throw new DuplexError('method_not_found', `Unknown method ${method}.`);
    }).then(
      result => this.respond(id, incoming, result === undefined ? null : result),
      (error: unknown) => {
        // Anything a handler threw but a public error is this runtime's panic.
        if (this.observer && !(error instanceof DuplexError)) {
          this.observe({ type: 'handler.panic', at: new Date(), method, value: describe(error), trace, family: this.family(method) });
        }
        this.respond(id, incoming, undefined, error instanceof DuplexError ? error : new DuplexError('internal', 'Request handler failed.'));
      },
    ).finally(() => {
      clearTimeout(incoming.timer);
      if (this.incoming.get(id) === incoming) this.incoming.delete(id);
    });
  }

  private respond(id: string, incoming: Incoming, result?: unknown, error?: DuplexError, outcome: Outcome = error ? 'error' : 'ok'): void {
    if (incoming.responded || this.incoming.get(id) !== incoming || !this.isOpen()) return;
    incoming.responded = true;
    clearTimeout(incoming.timer);
    const frame: Envelope = { version: 1, kind: 'response', id };
    if (error) frame.error = { code: error.code, message: error.message, ...(error.data === undefined ? {} : { data: error.data }) };
    else frame.result = result;
    // A response carries its request's trace, and mints none of its own.
    void this.send(traced(frame, incoming.trace), incoming.method).catch(error => this.fail(asError(error)));
    if (this.observer) {
      this.observe({ type: 'request.ended', at: new Date(), id, method: incoming.method, incoming: true, durationMs: Date.now() - incoming.started, outcome, errorCode: error?.code, trace: incoming.trace, family: this.family(incoming.method) });
    }
  }

  private event(name: string, data: unknown, bytes: number, trace?: Trace): void {
    if (this.events.length + Number(this.eventActive) >= this.limits.maxQueuedMessages) {
      if (this.observer) this.pressure(this.events.length + Number(this.eventActive), true);
      this.fail(new DuplexError('busy', 'Event consumer is stalled; queue limit reached.'));
      return;
    }
    this.events.push({ name, data, bytes, trace });
    this.drainEvents();
  }

  private drainEvents(): void {
    if (this.eventActive || !this.isOpen()) return;
    const event = this.events.shift();
    if (!event) return;
    this.eventActive = true;
    // Delivered when it reaches the listeners, not when its frame arrived.
    if (this.observer) this.observe({ type: 'event.delivered', at: new Date(), name: event.name, bytes: event.bytes, trace: event.trace, family: this.family(event.name) });
    const generation = this.generation;
    this.eventTimer = setTimeout(() => {
      if (this.observer) this.pressure(this.events.length, true);
      this.fail(new DuplexError('stalled_consumer', 'Event handler deadline exceeded.'));
    }, this.limits.writeTimeoutMs);
    const listeners = [...this.listeners];
    let index = 0;
    const finish = () => {
      if (generation !== this.generation) return;
      clearTimeout(this.eventTimer);
      this.eventTimer = undefined;
      this.eventActive = false;
      this.drainEvents();
    };
    const next = () => {
      while (index < listeners.length) {
        if (!this.isOpen() || generation !== this.generation) return;
        try {
          const result = listeners[index++](event.name, event.data);
          if (result && typeof result.then === 'function') {
            void result.then(next, () => {
              this.notifyError(new DuplexError('event_handler_failed', 'An event handler failed.'));
              next();
            });
            return;
          }
        } catch {
          this.notifyError(new DuplexError('event_handler_failed', 'An event handler failed.'));
        }
      }
      finish();
    };
    // A synchronous listener must finish synchronously: a native WebSocket can
    // deliver a large replay burst within one turn, before any microtask runs.
    next();
  }

  private fail(error: DuplexError, closeConnection = true, code = 4011, reason = CLOSE_REASON): void {
    const connection = this.connection;
    if (!connection) return;
    this.connection = undefined;
    this.state = 'disconnected';
    this.generation++;
    this.detach?.();
    this.detach = undefined;
    clearTimeout(this.writeTimer);
    this.writeTimer = undefined;
    clearTimeout(this.eventTimer);
    this.eventTimer = undefined;
    this.eventActive = false;
    this.events.length = 0;
    if (this.opening) {
      clearTimeout(this.opening.timer);
      this.opening.reject(error);
      this.opening = undefined;
    }
    for (const id of [...this.pending.keys()]) {
      const pending = this.takePending(id);
      if (!pending) continue;
      this.ended(id, pending, 'error', error.code);
      pending.reject(error);
    }
    for (const [id, request] of this.incoming) {
      clearTimeout(request.timer);
      request.controller.abort();
      if (this.observer && !request.responded) {
        this.observe({ type: 'request.ended', at: new Date(), id, method: request.method, incoming: true, durationMs: Date.now() - request.started, outcome: 'error', errorCode: error.code, trace: request.trace, family: this.family(request.method) });
      }
    }
    this.incoming.clear();
    for (const item of this.outgoing.splice(0)) item.reject(error);
    if (closeConnection) {
      // Browser close() restricts application codes to 3000–4999 (or 1000).
      try { connection.close(code, reason); } catch { /* Already closed. */ }
    }
    // Reported once everything it ended has been. The peer closes the
    // connection exactly when the close is its own.
    if (this.observer) this.observe({ type: 'connection.closed', at: new Date(), code, reason, local: closeConnection });
    this.notifyError(error);
    for (const listener of this.closedListeners) {
      try { listener(error); } catch { /* Observers cannot interrupt cleanup. */ }
    }
  }

  /**
   * Tells this peer's observer one event, the runtime's own or one of a layer
   * running over the peer, which is how a tunnel and a session observe — through
   * the peer they run over, rather than through an observer of their own. An
   * observer that is absent costs nothing, and one that throws interrupts nothing.
   */
  observe(event: ObserverEvent): void {
    try { this.observer?.observe(event); } catch { /* Observers cannot interrupt routing. */ }
  }

  private pressure(queued: number, stalled: boolean): void {
    this.observe({ type: 'backpressure', at: new Date(), queued, stalled, deadlineMs: this.limits.writeTimeoutMs });
  }

  /** Every outgoing call ends once, wherever it settles. */
  private ended(id: string, pending: Pending, outcome: Outcome, errorCode?: string): void {
    if (!this.observer) return;
    this.observe({ type: 'request.ended', at: new Date(), id, method: pending.method, incoming: false, durationMs: Date.now() - pending.started, outcome, errorCode, trace: pending.trace, family: this.family(pending.method) });
  }

  /** What a frame is about; a response or a cancel takes its request's name. */
  private nameOf(frame: Envelope): string {
    switch (frame.kind) {
      case 'request': return frame.method as string;
      case 'event': return frame.event as string;
      case 'response': return this.pending.get(frame.id as string)?.method ?? '';
      default: return this.incoming.get(frame.id as string)?.method ?? '';
    }
  }

  /** The label the caller gave this method or event name; an unlabelled name has none. */
  private family(name: string): string {
    const label = this.options.families?.[name];
    return typeof label === 'string' ? label : '';
  }

  private notifyError(error: DuplexError): void {
    try { this.options.onError?.(error); } catch { /* Observers cannot interrupt routing. */ }
  }
}

/**
 * One frame of the profile, validated by kind. Every kind may carry W3C trace
 * context; the members are kept on the envelope for a caller that propagates
 * them, and the peer itself reads neither. Not part of the package surface.
 */
export function decodeEnvelope(data: string, localPrefix: string, remotePrefix: string): Envelope {
  const value: unknown = JSON.parse(data);
  if (!isObject(value) || value.version !== 1) throw new Error();
  const frame: Envelope = value;
  switch (frame.kind) {
    case 'request':
      keys(frame, ['version', 'kind', 'id', 'method', 'params', ...TRACE]);
      requestID(frame.id, remotePrefix);
      requireName(frame.method, 'method');
      if (!Object.hasOwn(frame, 'params')) throw new Error();
      break;
    case 'response':
      keys(frame, ['version', 'kind', 'id', 'result', 'error', ...TRACE]);
      requestID(frame.id, localPrefix);
      if (Object.hasOwn(frame, 'result') === Object.hasOwn(frame, 'error')) throw new Error();
      if (Object.hasOwn(frame, 'error')) {
        if (!isObject(frame.error)) throw new Error();
        keys(frame.error, ['code', 'message', 'data']);
        requireName(frame.error.code, 'code');
        if (typeof frame.error.message !== 'string') throw new Error();
      }
      break;
    case 'cancel':
      keys(frame, ['version', 'kind', 'id', ...TRACE]);
      requestID(frame.id, remotePrefix);
      break;
    case 'event':
      keys(frame, ['version', 'kind', 'event', 'data', ...TRACE]);
      requireName(frame.event, 'event');
      if (!Object.hasOwn(frame, 'data')) throw new Error();
      break;
    default: throw new Error();
  }
  trace(frame);
  return frame;
}

function isObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}
function isWebSocketLike(value: FrameConnection | WebSocketLike): value is WebSocketLike {
  return typeof (value as WebSocketLike).readyState === 'number';
}
function keys(frame: Envelope, allowed: string[]): void {
  if (Object.keys(frame).some(key => !allowed.includes(key))) throw new Error('Unknown frame property.');
}
/** The reason a peer gives for a close of its own. */
const CLOSE_REASON = 'Duplex connection closed';
/** What a handler threw, as a string: never its params, and never a payload. */
function describe(value: unknown): string {
  try { return String(value); } catch { return '[unprintable value]'; }
}
/** W3C Trace Context, verbatim: an optional member of every kind, never of an error. */
const TRACE = ['traceparent', 'tracestate'];
const TRACEPARENT = /^[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$/;
function trace(frame: Envelope): void {
  if (Object.hasOwn(frame, 'traceparent') && (typeof frame.traceparent !== 'string' || !TRACEPARENT.test(frame.traceparent))) {
    throw new Error('Invalid traceparent.');
  }
  if (Object.hasOwn(frame, 'tracestate') && typeof frame.tracestate !== 'string') throw new Error('Invalid tracestate.');
}
function requireName(value: unknown, field: string): asserts value is string {
  if (typeof value !== 'string' || value.length === 0) throw new DuplexError('invalid_message', `${field} must be a nonempty string.`);
}
function requestID(value: unknown, prefix: string): void {
  if (typeof value !== 'string' || !value.startsWith(prefix) || !/^[1-9][0-9]{0,19}$/.test(value.slice(prefix.length))) throw new Error('Invalid request ID.');
}
function positiveInteger(value: number, name: string): void {
  if (!Number.isSafeInteger(value) || value <= 0) throw new DuplexError('invalid_options', `${name} must be a positive safe integer.`);
}
function asError(error: unknown, code = 'internal'): DuplexError {
  return error instanceof DuplexError ? error : new DuplexError(code, 'Duplex operation failed.');
}
