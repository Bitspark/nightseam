import { DuplexError } from './error.ts';
import type { Observer, ObserverEvent } from './observer.ts';
import type { Trace } from './trace.ts';

/** @internal Diagnostics cannot determine whether a model operation succeeds. */
export function observeWire(observer: Observer | undefined, event: ObserverEvent): void {
  try {
    observer?.observe(event);
  } catch {
    /* A diagnostic fails alone. */
  }
}

/** @internal Uses the helper's existing completion; it retains no routing state. */
export function observeWireRequest(
  observer: Observer | undefined,
  family: string | undefined,
  id: string,
  method: string,
  incoming: boolean,
  trace: Trace | undefined,
): (error?: unknown, outcome?: 'ok' | 'error' | 'cancelled' | 'timeout') => void {
  if (!observer) return () => {};
  const started = performance.now(),
    label = family ?? '';
  let finished = false;
  observeWire(observer, { type: 'request.started', at: new Date(), id, method, incoming, trace, family: label });
  return (error, outcome = error === undefined ? 'ok' : 'error') => {
    if (finished) return;
    finished = true;
    observeWire(observer, {
      type: 'request.ended',
      at: new Date(),
      id,
      method,
      incoming,
      trace,
      family: label,
      durationMs: performance.now() - started,
      outcome,
      ...(error === undefined ? {} : { errorCode: error instanceof DuplexError ? error.code : 'internal' }),
    });
  };
}
