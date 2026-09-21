import type { Message, Path, Receiver, ReturnAddress, Wire } from '@nightseam/duplex';
import { DuplexError } from './error.ts';
import { IDENTITY_METHOD, checkIdentity, identityHandler, type DeclarationIdentity } from './identity.ts';
import { DUPLEX_DEFAULTS, positiveInteger, type PeerOptions } from './peer.ts';
import { callWire, handleWire, publicError, response, type WireCallOptions } from './wire.ts';

/** Synchronous receiver installation followed by a bounded identity check and
 * model binding. Closing this preparation never closes its source carrier. */
export interface IdentityPreparation {
  readonly wire: Wire;
  check(options?: WireCallOptions): Promise<void>;
  ready(): void;
  close(): void;
}

interface Registration {
  receiver: Receiver;
  active: boolean;
  detached?: () => void;
  closed: boolean;
}

interface DeferredDelivery {
  registration: Registration;
  path: Path;
  message: Message;
  cancelled: boolean;
  retired: boolean;
  forwarded: boolean;
}

/** Installs receivers directly on the source, without another receive queue.
 * requestTimeoutMs bounds preparation from creation through model binding;
 * maxConcurrentHandlers also bounds deferral on arbitrary conforming wires. */
export function prepareIdentity(
  source: Wire,
  expected: DeclarationIdentity,
  options: PeerOptions = {},
): IdentityPreparation {
  // Reuse identity's native validation and snapshot rules without making IO.
  const local = identityHandler(expected)(expected.digest === undefined ? { path: expected.path } : expected);
  const timeout = positiveInteger(options.requestTimeoutMs ?? DUPLEX_DEFAULTS.requestTimeoutMs, 'requestTimeoutMs');
  const limit = positiveInteger(
    options.maxConcurrentHandlers ?? DUPLEX_DEFAULTS.maxConcurrentHandlers,
    'maxConcurrentHandlers',
  );
  const registrations = new Set<Registration>();
  const pending = new Map<ReturnAddress, Map<string, DeferredDelivery>>();
  let count = 0;
  let started = false,
    checked = false,
    ready = false;
  let failure: DuplexError | undefined;
  let identityDetach: (() => void) | undefined;
  let release!: () => void;
  const released = new Promise<void>((resolve) => {
    release = resolve;
  });
  const abort = new AbortController();
  const timer = setTimeout(() => fail(new DuplexError('cancelled', 'Declaration interpretation timed out.')), timeout);

  function fail(error: unknown): void {
    if (failure) return;
    failure = publicError(error);
    clearTimeout(timer);
    release();
    abort.abort();
    identityDetach?.();
    for (const calls of pending.values()) for (const delivery of calls.values()) dispatch(delivery);
    const owned = [...registrations];
    registrations.clear();
    for (const registration of owned) {
      registration.active = false;
      registration.detached?.();
      if (!registration.closed) {
        registration.closed = true;
        try {
          registration.receiver.closed?.(1000, 'interpretation ended');
        } catch {
          /* Cleanup still owns the remaining registrations. */
        }
      }
    }
  }

  function retire(delivery: DeferredDelivery): void {
    if (delivery.message.frame.kind !== 'request' || !delivery.message.return) return;
    const calls = pending.get(delivery.message.return);
    if (calls?.get(delivery.message.frame.id) !== delivery) return;
    delivery.retired = true;
    calls.delete(delivery.message.frame.id);
    if (!calls.size) pending.delete(delivery.message.return);
    count--;
  }

  function dispatch(delivery: DeferredDelivery): void {
    if (delivery.cancelled || delivery.retired) return;
    const { registration, path, message } = delivery;
    if (failure || !registration.active) {
      response(message, undefined, failure ?? new DuplexError('disconnected', 'Interpretation ended.'));
      retire(delivery);
      return;
    }
    try {
      delivery.forwarded = true;
      const result = registration.receiver.message?.(path, message);
      if (result) void result.catch((error: unknown) => response(message, undefined, error));
    } catch (error) {
      response(message, undefined, error);
    } finally {
      retire(delivery);
    }
  }

  function deliver(registration: Registration, path: Path, message: Message): void | Promise<void> {
    const frame = message.frame;
    if (frame.kind === 'cancel') {
      const delivery = message.return && pending.get(message.return)?.get(frame.id);
      if (delivery) {
        if (delivery.forwarded) return delivery.registration.receiver.message?.(path, message);
        delivery.cancelled = true;
        retire(delivery);
        response(delivery.message, undefined, new DuplexError('cancelled', 'Request was cancelled.'));
      } else if (ready && !failure && registration.active) {
        return registration.receiver.message?.(path, message);
      }
      return;
    }
    if (failure || !registration.active) {
      response(message, undefined, failure ?? new DuplexError('disconnected', 'Interpretation ended.'));
      return;
    }
    if (ready) return registration.receiver.message?.(path, message);
    if (frame.kind === 'event') {
      // Await on the root's existing serial event delivery. A refused event
      // resolves successfully so RegisterWire does not close a shared carrier.
      return released.then(() => {
        if (!failure && registration.active) return registration.receiver.message?.(path, message);
      });
    }
    if (frame.kind !== 'request' || !message.return) return;
    let calls = pending.get(message.return);
    if (calls?.has(frame.id) || count >= limit) {
      response(
        message,
        undefined,
        new DuplexError(calls?.has(frame.id) ? 'invalid_message' : 'busy', 'Deferred model request refused.'),
      );
      return;
    }
    if (!calls) {
      calls = new Map();
      pending.set(message.return, calls);
    }
    const delivery: DeferredDelivery = {
      registration,
      path,
      message,
      cancelled: false,
      retired: false,
      forwarded: false,
    };
    calls.set(frame.id, delivery);
    count++;
    // Do not hold a root's request callback: a local root must still dispatch
    // later identity requests. Only admitted deliveries are deferred, and
    // their original return address preserves cancellation until release.
    // Keep no promise callback on `released` per request: cancelled requests
    // must release their memory immediately rather than accumulate until Ready.
  }

  const wire: Wire = {
    send(path, message) {
      if (failure) throw failure;
      if (!ready) throw new DuplexError('busy', 'Declaration interpretation is not ready.');
      source.send(path, message);
    },
    receive(path, receiver) {
      if (failure) throw failure;
      if (!receiver.message) throw new DuplexError('invalid_message', 'A wire receiver requires a callback.');
      const registration: Registration = { receiver, active: true, closed: false };
      registrations.add(registration);
      try {
        registration.detached = source.receive(path, {
          ...(receiver.namespace === undefined ? {} : { namespace: receiver.namespace }),
          message: (path, message) => deliver(registration, path, message),
          closed: () => fail(new DuplexError('disconnected', 'Interpretation carrier ended.')),
        });
      } catch (error) {
        registrations.delete(registration);
        registration.active = false;
        throw error;
      }
      if (failure) {
        registration.detached();
        throw failure;
      }
      return () => {
        registration.active = false;
        registrations.delete(registration);
        registration.detached?.();
      };
    },
    close(code, reason) {
      fail(new DuplexError('disconnected', 'Interpretation closed.'));
      source.close(code, reason);
    },
  };

  try {
    identityDetach = handleWire(
      {
        send: (path, message) => source.send(path, message),
        receive: (path, receiver) =>
          source.receive(path, {
            ...receiver,
            closed: (code, reason) => {
              try {
                receiver.closed?.(code, reason);
              } finally {
                fail(new DuplexError('disconnected', 'Interpretation carrier ended.'));
              }
            },
          }),
        close: (code, reason) => source.close(code, reason),
      },
      [IDENTITY_METHOD],
      identityHandler(local),
    );
  } catch (error) {
    fail(error);
    throw error;
  }
  if (failure) {
    identityDetach();
    throw failure;
  }

  return {
    wire,
    async check(options = {}) {
      if (failure) throw failure;
      if (started) throw new Error('Identity check already started.');
      started = true;
      try {
        await checkIdentity((method, params, callOptions) => callWire(source, [method], params, callOptions), local, {
          ...options,
          signal: options.signal ? AbortSignal.any([options.signal, abort.signal]) : abort.signal,
        });
      } catch (error) {
        fail(error);
      }
      if (failure) throw failure;
      checked = true;
    },
    ready() {
      if (failure) throw failure;
      if (!checked) throw new Error('Identity has not been checked.');
      if (ready) throw new Error('Identity preparation is already ready.');
      ready = true;
      clearTimeout(timer);
      release();
      for (const calls of pending.values())
        for (const delivery of calls.values()) queueMicrotask(() => dispatch(delivery));
    },
    close() {
      fail(new DuplexError('disconnected', 'Interpretation closed.'));
    },
  };
}
