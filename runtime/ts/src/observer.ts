import type { Trace } from './trace.ts';

/**
 * What a peer tells an observer about the traffic it carries. It emits and
 * never aggregates, and chooses no backend: an observer sees names, ids,
 * sizes, durations, outcomes and close codes — never a payload. Diagnostic
 * logging is one observer among others; the runtime writes to no logger.
 *
 * An event that concerns a frame carries that frame's trace and the family of
 * the name it names; a connection or backpressure event concerns no frame and
 * carries neither, stated rather than nullable.
 */
export type ObserverEvent =
  | { type: 'connection.opened'; at: Date; role: 'client' | 'server' }
  | { type: 'connection.closed'; at: Date; code: number; reason: string; local: boolean }
  | { type: 'frame.sent' | 'frame.received'; at: Date; kind: string; name: string; bytes: number; id?: string; trace?: Trace; family: string }
  | { type: 'request.started'; at: Date; id: string; method: string; incoming: boolean; trace?: Trace; family: string }
  | { type: 'request.ended'; at: Date; id: string; method: string; incoming: boolean; durationMs: number; outcome: 'ok' | 'error' | 'cancelled' | 'timeout'; errorCode?: string; trace?: Trace; family: string }
  | { type: 'event.emitted' | 'event.delivered'; at: Date; name: string; bytes: number; trace?: Trace; family: string }
  | { type: 'backpressure'; at: Date; queued: number; stalled: boolean; deadlineMs: number }
  | { type: 'handler.panic'; at: Date; method: string; value: string; trace?: Trace; family: string };

/** One interface, one method; whatever runs over a peer inherits the one it was given. */
export interface Observer { observe(event: ObserverEvent): void }

/** The default. A peer given none observes nothing and pays for nothing. */
export const NO_OBSERVER: Observer = Object.freeze({ observe(): void { /* Nothing is aggregated, here or anywhere. */ } });
