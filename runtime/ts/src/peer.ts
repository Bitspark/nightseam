import { webSocketConnection } from '@nightseam/duplex';
import type { Frame, FrameConnection, WebSocketLike } from '@nightseam/duplex';
import { defaultPropagator, traceOf, traced } from './trace.ts';
import type { Propagator, Trace } from './trace.ts';

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
}

type Timer = ReturnType<typeof setTimeout>;
/** A decoded JSON envelope; the connection beneath carries it as a text frame. */
type Envelope = Record<string, unknown>;
interface Pending {
  resolve: (value: unknown) => void;
  reject: (error: DuplexError) => void;
  timer?: Timer;
  removeAbort?: () => void;
}
interface Incoming {
  controller: AbortController;
  timer: Timer;
  responded: boolean;
  /** The request's trace; its response, and nothing else, carries it back. */
  trace?: Trace;
}
interface Outgoing {
  text: string;
  started: number;
  sent: boolean;
  resolve: () => void;
  reject: (error: DuplexError) => void;
}
interface QueuedEvent { name: string; data: unknown }

/**
 * A bounded full-duplex peer. Message routing never awaits application handlers.
 * Calls are not retried, and connections are never reopened automatically.
 * The caller owns endpoint authentication and authorization of incoming methods.
 */
export class DuplexPeer {
  private readonly options: PeerOptions;
  private readonly limits: typeof DUPLEX_DEFAULTS;
  private readonly propagator: Propagator;
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
    const current = () => this.connection === frames;
    this.detach = frames.listen({
      open: () => {
        if (!current()) return;
        this.state = 'connected';
        if (this.opening) {
          clearTimeout(this.opening.timer);
          this.opening.resolve();
          this.opening = undefined;
        }
      },
      frame: frame => { if (current()) this.receive(frame); },
      close: () => {
        if (current()) this.fail(new DuplexError('disconnected', 'Connection closed; outstanding call outcomes may be unknown.'), false);
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
      const pending: Pending = { resolve: value => resolve(value as T), reject };
      this.pending.set(id, pending);
      const cancel = (error: DuplexError) => {
        if (!this.takePending(id)) return;
        reject(error);
        void this.send(traced({ version: 1, kind: 'cancel', id }, trace)).catch(() => {});
      };
      pending.timer = setTimeout(() => cancel(new DuplexError('request_timeout', `Call ${method} timed out; its outcome may be unknown.`)), options.timeoutMs ?? this.limits.requestTimeoutMs);
      if (options.signal) {
        const abort = () => cancel(new DuplexError('cancelled', 'Call was cancelled; its outcome may be unknown.'));
        options.signal.addEventListener('abort', abort, { once: true });
        pending.removeAbort = () => options.signal!.removeEventListener('abort', abort);
      }
      void this.send(traced({ version: 1, kind: 'request', id, method, params }, trace)).catch(error => {
        this.takePending(id)?.reject(asError(error, 'send_failed'));
      });
    });
  }

  /** Resolves when accepted by the socket and its reported byte buffer drains. */
  emit(event: string, data: unknown = null, options: EmitOptions = {}): Promise<void> {
    try { requireName(event, 'event'); } catch (error) { return Promise.reject(error); }
    return this.send(traced({ version: 1, kind: 'event', event, data }, this.propagator.inject(options.context)));
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

  private send(envelope: Envelope): Promise<void> {
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
    if (new TextEncoder().encode(text).byteLength > this.limits.maxFrameBytes) {
      return Promise.reject(new DuplexError('frame_too_large', 'Outgoing frame exceeds the size limit.'));
    }
    if (this.outgoing.length >= this.limits.maxQueuedMessages) {
      const error = new DuplexError('busy', 'Output consumer is stalled; queue limit reached.');
      this.fail(error);
      return Promise.reject(error);
    }
    return new Promise<void>((resolve, reject) => {
      this.outgoing.push({ text, started: Date.now(), sent: false, resolve, reject });
      this.flush();
    });
  }

  private flush(): void {
    if (this.writeTimer || !this.isOpen()) return;
    const connection = this.connection!;
    while (this.outgoing.length) {
      const item = this.outgoing[0];
      if (Date.now() - item.started >= this.limits.writeTimeoutMs) {
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
    if (new TextEncoder().encode(data).byteLength > this.limits.maxFrameBytes) {
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
    switch (frame.kind) {
      case 'response': {
        const pending = this.takePending(frame.id as string);
        if (!pending) return; // A cancellation or deadline may precede a late response.
        if (isObject(frame.error)) pending.reject(new DuplexError(frame.error.code as string, frame.error.message as string, frame.error.data));
        else pending.resolve(frame.result);
        break;
      }
      case 'cancel': {
        const incoming = this.incoming.get(frame.id as string);
        if (incoming) {
          incoming.controller.abort();
          this.respond(frame.id as string, incoming, undefined, new DuplexError('cancelled', 'Request was cancelled.'));
        }
        break;
      }
      case 'request': this.request(frame.id as string, frame.method as string, frame.params, traceOf(frame)); break;
      case 'event': this.event(frame.event as string, frame.data); break;
    }
  }

  private request(id: string, method: string, params: unknown, trace?: Trace): void {
    if (this.incoming.has(id)) {
      this.fail(new DuplexError('invalid_message', 'An incoming request ID is already active.'));
      return;
    }
    if (this.incoming.size >= this.limits.maxIncomingRequests) {
      void this.send(traced({ version: 1, kind: 'response', id, error: { code: 'busy', message: 'Incoming request limit reached.' } }, trace)).catch(error => this.fail(asError(error)));
      return;
    }
    const controller = new AbortController();
    const incoming: Incoming = {
      controller,
      responded: false,
      trace,
      timer: setTimeout(() => {
        controller.abort();
        this.respond(id, incoming, undefined, new DuplexError('request_timeout', 'Request deadline exceeded.'));
      }, this.limits.requestTimeoutMs),
    };
    this.incoming.set(id, incoming);
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
      error => this.respond(id, incoming, undefined, error instanceof DuplexError ? error : new DuplexError('internal', 'Request handler failed.')),
    ).finally(() => {
      clearTimeout(incoming.timer);
      if (this.incoming.get(id) === incoming) this.incoming.delete(id);
    });
  }

  private respond(id: string, incoming: Incoming, result?: unknown, error?: DuplexError): void {
    if (incoming.responded || this.incoming.get(id) !== incoming || !this.isOpen()) return;
    incoming.responded = true;
    clearTimeout(incoming.timer);
    const frame: Envelope = { version: 1, kind: 'response', id };
    if (error) frame.error = { code: error.code, message: error.message, ...(error.data === undefined ? {} : { data: error.data }) };
    else frame.result = result;
    // A response carries its request's trace, and mints none of its own.
    void this.send(traced(frame, incoming.trace)).catch(error => this.fail(asError(error)));
  }

  private event(name: string, data: unknown): void {
    if (this.events.length + Number(this.eventActive) >= this.limits.maxQueuedMessages) {
      this.fail(new DuplexError('busy', 'Event consumer is stalled; queue limit reached.'));
      return;
    }
    this.events.push({ name, data });
    this.drainEvents();
  }

  private drainEvents(): void {
    if (this.eventActive || !this.isOpen()) return;
    const event = this.events.shift();
    if (!event) return;
    this.eventActive = true;
    const generation = this.generation;
    this.eventTimer = setTimeout(() => this.fail(new DuplexError('stalled_consumer', 'Event handler deadline exceeded.')), this.limits.writeTimeoutMs);
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

  private fail(error: DuplexError, closeConnection = true, code = 4011): void {
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
    for (const id of this.pending.keys()) this.takePending(id)?.reject(error);
    for (const request of this.incoming.values()) {
      clearTimeout(request.timer);
      request.controller.abort();
    }
    this.incoming.clear();
    for (const item of this.outgoing.splice(0)) item.reject(error);
    if (closeConnection) {
      // Browser close() restricts application codes to 3000–4999 (or 1000).
      try { connection.close(code, 'Duplex connection closed'); } catch { /* Already closed. */ }
    }
    this.notifyError(error);
    for (const listener of this.closedListeners) {
      try { listener(error); } catch { /* Observers cannot interrupt cleanup. */ }
    }
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
