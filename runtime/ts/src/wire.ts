import { encodePath, WireError } from '@nightseam/duplex';
import type { Message, Path, ProfileFrame, Receiver, ReturnAddress, Wire } from '@nightseam/duplex';
import { DuplexError, UnpublishedError } from './error.ts';
import { carrying } from './envelope.ts';
import { scalarJSON } from './unicode.ts';
import { defaultPropagator, traceOf } from './trace.ts';
import type { Trace, TraceContext } from './trace.ts';
import type { DuplexPeer, Meta, RequestContext } from './peer.ts';

interface WireDispatchContext {
  context: RequestContext;
  panic: (error: unknown) => void;
}
// This is a local capability association, never a field a caller can serialize
// or supply as ambient outgoing metadata. Keep non-enumerable verified values.
const dispatchContexts = new WeakMap<ReturnAddress, WireDispatchContext>();

/** Context beside a wire request, independent of its concrete carrier. */
export interface WireRequestContext extends TraceContext {
  wire: Wire;
  signal: AbortSignal;
  requestId: string;
  meta?: Meta;
}
export interface WireCallOptions {
  signal?: AbortSignal;
  timeoutMs?: number;
  context?: TraceContext;
  meta?: Meta;
}
export interface WireEmitOptions {
  context?: TraceContext;
  meta?: Meta;
}
export interface WireEventContext extends TraceContext {
  wire: Wire;
  meta?: Meta;
}
export type WireHandler = (params: unknown, context: WireRequestContext) => unknown | Promise<unknown>;
export type WireEventListener = (data: unknown, context: WireEventContext) => void | Promise<void>;

/** @internal The completion/deadline primitive shared by Peer.call and CallWire. */
export function requestCompletion<T>() {
  let settled = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let detach: (() => void) | undefined;
  let resolve!: (value: T) => void;
  let reject!: (error: DuplexError) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  const cleanup = () => {
    clearTimeout(timer);
    detach?.();
    detach = undefined;
  };
  return {
    promise,
    get settled() {
      return settled;
    },
    cleanup,
    resolve: (value: T) => {
      if (!settled) {
        settled = true;
        cleanup();
        resolve(value);
      }
    },
    reject: (error: DuplexError) => {
      if (!settled) {
        settled = true;
        cleanup();
        reject(error);
      }
    },
    wait: (
      signal: AbortSignal | undefined,
      timeoutMs: number,
      method: string,
      cancel: (error: DuplexError, outcome: 'timeout' | 'cancelled') => void,
    ) => {
      if (settled) return;
      timer = setTimeout(
        () =>
          cancel(
            new DuplexError('request_timeout', `Call ${method} timed out; its outcome may be unknown.`),
            'timeout',
          ),
        timeoutMs,
      );
      if (signal) {
        const abort = () =>
          cancel(new DuplexError('cancelled', 'Call was cancelled; its outcome may be unknown.'), 'cancelled');
        signal.addEventListener('abort', abort, { once: true });
        detach = () => signal.removeEventListener('abort', abort);
        if (signal.aborted) abort();
      }
    },
  };
}

function snapshot<T>(value: T): T {
  try {
    const encoded = JSON.stringify(value, (_key, member: unknown) => {
      if (
        typeof member === 'function' ||
        typeof member === 'symbol' ||
        (typeof member === 'number' && !Number.isFinite(member))
      )
        throw new Error('Not JSON.');
      return member;
    });
    scalarJSON(encoded);
    return JSON.parse(encoded) as T;
  } catch {
    throw new DuplexError('invalid_message', 'Frame must contain serializable JSON values.');
  }
}

function publicError(error: unknown): DuplexError {
  // Reconstructing public data strips local publication proof after dispatch.
  return error instanceof DuplexError
    ? new DuplexError(error.code, error.message, error.data)
    : new DuplexError('internal', 'Request handler failed.');
}
function response(request: Message, result?: unknown, error?: unknown): void {
  if (request.frame.kind !== 'request' || !request.return) return;
  let payload: { result: unknown } | { error: { code: string; message: string; data?: unknown } };
  try {
    if (error !== undefined) {
      const refused = publicError(error);
      payload = snapshot({
        error: {
          code: refused.code,
          message: refused.message,
          ...(refused.data === undefined ? {} : { data: refused.data }),
        },
      });
    } else payload = { result: snapshot(result === undefined ? null : result) };
  } catch {
    payload = { error: { code: 'internal', message: 'Response could not be encoded' } };
  }
  const frame: ProfileFrame = {
    version: 1,
    kind: 'response',
    id: request.frame.id,
    ...payload,
    ...traceOf(request.frame),
  };
  try {
    request.return.wire.send([], { frame });
  } catch {
    /* The caller may already have cancelled or ended. */
  }
}

/** Calls through the shared request primitive; no new peer or channel is made. */
export function callWire<T = unknown>(
  wire: Wire,
  path: Path,
  params: unknown = {},
  options: WireCallOptions = {},
): Promise<T> {
  return callWireTraced(wire, path, params, options, defaultPropagator.inject(options.context));
}
function callWireTraced<T>(
  wire: Wire,
  path: Path,
  params: unknown,
  options: WireCallOptions,
  trace?: Trace,
  dispatch?: WireDispatchContext,
): Promise<T> {
  let name: string;
  let frame: ProfileFrame;
  try {
    name = encodePath(path);
    if (options.timeoutMs !== undefined && (!Number.isSafeInteger(options.timeoutMs) || options.timeoutMs <= 0))
      throw new DuplexError('invalid_options', 'timeoutMs must be a positive safe integer.');
    if (options.signal?.aborted) throw new DuplexError('cancelled', 'Call was cancelled before sending.');
    frame = snapshot(
      carrying({ version: 1, kind: 'request', id: 'c:1', params, ...trace }, options.meta),
    ) as unknown as ProfileFrame;
  } catch (error) {
    return Promise.reject(new UnpublishedError(error));
  }
  const completion = requestCompletion<T>();
  const returning: Wire = {
    send: (suffix, message) => {
      if (suffix.length || message.frame.kind !== 'response' || message.frame.id !== 'c:1')
        throw new DuplexError('invalid_message', 'Invalid wire response.');
      if (completion.settled) throw new WireError('closed');
      if (message.frame.error)
        completion.reject(
          new DuplexError(message.frame.error.code, message.frame.error.message, message.frame.error.data),
        );
      else completion.resolve(message.frame.result as T);
    },
    receive: () => {
      throw new WireError('receiver_exists');
    },
    close: () => completion.reject(new DuplexError('disconnected', 'Connection ended; outcome may be unknown.')),
  };
  const address: ReturnAddress = { wire: returning };
  if (dispatch) {
    dispatchContexts.set(address, dispatch);
    void completion.promise.then(
      () => dispatchContexts.delete(address),
      () => dispatchContexts.delete(address),
    );
  }
  try {
    wire.send(path, { frame, return: address });
  } catch (error) {
    completion.reject(new UnpublishedError(error));
  }
  completion.wait(options.signal, options.timeoutMs ?? 30_000, name, (error) => {
    if (completion.settled) return;
    completion.reject(error);
    try {
      wire.send(path, { frame: { version: 1, kind: 'cancel', id: 'c:1', ...trace }, return: address });
    } catch {
      /* Cancellation is best effort and never extends the caller's wait. */
    }
  });
  return completion.promise;
}

/** Registers one operation; application code runs after the delivering turn. */
export function handleWire(wire: Wire, path: Path, handler: WireHandler): () => void {
  const incoming = new Map<ReturnAddress, Map<string, AbortController>>();
  const stop = () => {
    for (const calls of incoming.values()) for (const controller of calls.values()) controller.abort();
  };
  const detach = wire.receive(path, {
    closed: stop,
    message: (_path, message) => {
      const frame = message.frame;
      if ((frame.kind !== 'request' && frame.kind !== 'cancel') || !message.return) return;
      let calls = incoming.get(message.return);
      if (frame.kind === 'cancel') {
        calls?.get(frame.id)?.abort();
        return;
      }
      if (calls?.has(frame.id)) {
        response(message, undefined, new DuplexError('invalid_message', 'Duplicate active request identifier.'));
        return;
      }
      if (!calls) {
        calls = new Map();
        incoming.set(message.return, calls);
      }
      const controller = new AbortController();
      calls.set(frame.id, controller);
      const dispatch = dispatchContexts.get(message.return);
      const context: WireRequestContext = dispatch
        ? (Object.create(dispatch.context) as WireRequestContext)
        : ({
            ...(frame.meta ? { meta: { ...frame.meta } } : {}),
          } as WireRequestContext);
      Object.defineProperties(context, {
        wire: { value: wire, enumerable: true },
        signal: {
          value: dispatch ? AbortSignal.any([controller.signal, dispatch.context.signal]) : controller.signal,
          enumerable: true,
        },
        requestId: { value: dispatch?.context.requestId ?? frame.id, enumerable: true },
      });
      if (!dispatch) defaultPropagator.extract(context, traceOf(frame));
      void Promise.resolve()
        .then(() => {
          if (context.signal.aborted) throw new DuplexError('cancelled', 'Request was cancelled.');
          return handler(frame.params, context);
        })
        .then(
          (result) =>
            response(
              message,
              result,
              context.signal.aborted ? new DuplexError('cancelled', 'Request was cancelled.') : undefined,
            ),
          (error: unknown) => {
            if (!(error instanceof DuplexError)) dispatch?.panic(error);
            response(message, undefined, publicError(error));
          },
        )
        .finally(() => {
          calls!.delete(frame.id);
          if (!calls!.size) incoming.delete(message.return!);
        });
    },
  });
  return () => {
    detach();
    stop();
  };
}

/** Emits a relative event; return means accepted, never consumed. */
export function emitWire(wire: Wire, path: Path, data: unknown = null, options: WireEmitOptions = {}): void {
  try {
    encodePath(path);
    const frame = snapshot(
      carrying({ version: 1, kind: 'event', data, ...defaultPropagator.inject(options.context) }, options.meta),
    ) as unknown as ProfileFrame;
    wire.send(path, { frame });
  } catch (error) {
    throw new UnpublishedError(error);
  }
}

/** The root's existing serial event dispatcher awaits an async listener. */
export function onWireEvent(wire: Wire, path: Path, listener: WireEventListener): () => void {
  return wire.receive(path, {
    message: (_path, message) => {
      if (message.frame.kind === 'request') {
        response(message, undefined, new DuplexError('method_not_found', 'An event has no request handler.'));
        return;
      }
      if (message.frame.kind !== 'event') return;
      const context: WireEventContext = { wire, ...(message.frame.meta ? { meta: { ...message.frame.meta } } : {}) };
      defaultPropagator.extract(context, traceOf(message.frame));
      return listener(message.frame.data, context);
    },
  });
}

/** @internal Hooks retain all carrier ownership in the peer. */
export interface PeerWireOptions {
  queueCapacity: number;
  maxPendingRequests: number;
  requestTimeoutMs: number;
  fail: (error: DuplexError) => void;
  pressure: (waiting: number) => void;
  panic: (method: string, error: unknown, trace?: Trace) => void;
  close: (code: number, reason: string) => void;
}

/** @internal One bridge per peer; selection never constructs another. */
export function peerWire(peer: DuplexPeer, options: PeerWireOptions): Wire {
  const queued: { path: Path; message: Message }[] = [];
  const incoming = new Map<ReturnAddress, Map<string, AbortController>>();
  const receivers = new Map<string, { receiver: Receiver; detach: () => void }>();
  let active = 0,
    scheduled = false,
    ended = false;
  const endError = () => new DuplexError('disconnected', 'Connection ended; outcome may be unknown.');
  peer.onClose(() => {
    ended = true;
    for (const delivery of queued.splice(0)) response(delivery.message, undefined, endError());
    for (const calls of incoming.values()) for (const controller of calls.values()) controller.abort();
    const ending = [...receivers.values()];
    for (const { detach } of ending) detach();
    for (const { receiver } of ending) {
      try {
        receiver.closed?.(1001, 'peer ended');
      } catch {
        /* One receiver cannot interrupt another's cleanup. */
      }
    }
  });
  const drain = () => {
    scheduled = false;
    while (queued.length && !ended) {
      const { path, message } = queued.shift()!;
      const frame = message.frame;
      if (frame.kind === 'cancel') {
        incoming.get(message.return!)?.get(frame.id)?.abort();
        continue;
      }
      const name = encodePath(path);
      if (frame.kind === 'event') {
        // Invoke admission now, in wire order; only completion is asynchronous.
        void peer
          .emit(name, frame.data, {
            context: { trace: traceOf(frame) },
            meta: frame.meta ? { ...frame.meta } : undefined,
          })
          .catch((error: unknown) => options.fail(publicError(error)));
        continue;
      }
      if (frame.kind !== 'request') continue;
      let calls = incoming.get(message.return!);
      if (calls?.has(frame.id) || active >= options.maxPendingRequests) {
        response(
          message,
          undefined,
          new DuplexError(calls?.has(frame.id) ? 'invalid_message' : 'busy', 'Outstanding wire call refused.'),
        );
        continue;
      }
      if (!calls) {
        calls = new Map();
        incoming.set(message.return!, calls);
      }
      const controller = new AbortController();
      calls.set(frame.id, controller);
      active++;
      // Peer.call allocates the carrier id and enqueues before it returns.
      const pending = peer.call(name, frame.params, {
        signal: controller.signal,
        context: { trace: traceOf(frame) },
        meta: frame.meta ? { ...frame.meta } : undefined,
      });
      void pending
        .then(
          (value) => response(message, value),
          (error: unknown) => response(message, undefined, error),
        )
        .finally(() => {
          calls!.delete(frame.id);
          active--;
          if (!calls!.size) incoming.delete(message.return!);
        });
    }
  };
  const wire: Wire = {
    send: (path, message) => {
      const name = encodePath(path);
      if (ended || peer.status !== 'connected') throw endError();
      const frame = message.frame;
      if (!name && (frame.kind === 'request' || frame.kind === 'event'))
        throw new DuplexError('invalid_message', 'A root wire operation requires a nonempty path.');
      if ((frame.kind === 'request' || frame.kind === 'cancel') && !message.return?.wire)
        throw new DuplexError('invalid_message', 'A wire request or cancellation requires a return address.');
      if (frame.kind !== 'request' && frame.kind !== 'event' && frame.kind !== 'cancel')
        throw new DuplexError('invalid_message', "A response is sent to its request's return address.");
      const saved = snapshot(frame);
      if (queued.length >= options.queueCapacity) {
        const error = new DuplexError('busy', 'Output consumer is stalled; queue limit reached.');
        options.pressure(queued.length);
        options.fail(error);
        throw error;
      }
      queued.push({ path: [...path], message: { frame: saved, return: message.return } });
      if (!scheduled) {
        scheduled = true;
        queueMicrotask(drain);
      }
    },
    receive: (path, receiver) => {
      if (ended) throw endError();
      const name = encodePath(path);
      if (!name || !receiver.message)
        throw new DuplexError('invalid_message', 'A wire receiver requires a nonempty operation path and callback.');
      if (receivers.has(name)) throw new WireError('receiver_exists');
      const selected = [...path];
      const target: Wire = {
        send: (suffix, message) => {
          try {
            const result = receiver.message!(suffix, message);
            if (result) void result.catch((error: unknown) => response(message, undefined, publicError(error)));
          } catch (error) {
            response(message, undefined, publicError(error));
          }
        },
        receive: () => {
          throw new WireError('receiver_exists');
        },
        close: () => {},
      };
      const removeHandler = peer.handle(name, (params, context) =>
        callWireTraced(
          target,
          selected,
          params,
          {
            signal: context.signal,
            timeoutMs: options.requestTimeoutMs,
            meta: context.meta,
          },
          context.trace,
          { context, panic: (error) => options.panic(name, error, context.trace) },
        ),
      );
      const removeListener = peer.onEvent(name, (data, context) =>
        receiver.message!(selected, {
          frame: { version: 1, kind: 'event', data, ...context.trace, ...(context.meta ? { meta: context.meta } : {}) },
        }),
      );
      const registration = {
        receiver,
        detach: () => {
          if (receivers.get(name) !== registration) return;
          receivers.delete(name);
          removeHandler();
          removeListener();
        },
      };
      receivers.set(name, registration);
      return registration.detach;
    },
    close: (code = 1000, reason = '') => options.close(code, reason),
  };
  return wire;
}
