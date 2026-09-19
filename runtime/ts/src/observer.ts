import type { Trace } from './trace.ts';

/**
 * The events a peer tells an observer about, one member per type, keyed by the
 * type it carries. It is an interface rather than a union so that a layer over
 * the peer adds its own by declaration merging —
 *
 * ```ts
 * declare module '@nightseam/runtime' {
 *   interface ObserverEvents { 'channel.opened': { type: 'channel.opened'; at: Date; family: string; id: number } }
 * }
 * ```
 *
 * — and a consumer's `switch (event.type)` stays exhaustive over every layer it
 * imports, the runtime's ten events and whatever the tunnel and the session add
 * to them. The runtime declares these ten and no more.
 */
export interface ObserverEvents {
  'connection.opened': { type: 'connection.opened'; at: Date; role: 'client' | 'server' };
  'connection.closed': { type: 'connection.closed'; at: Date; code: number; reason: string; local: boolean };
  'frame.sent': {
    type: 'frame.sent';
    at: Date;
    kind: string;
    name: string;
    bytes: number;
    id?: string;
    trace?: Trace;
    family: string;
  };
  'frame.received': {
    type: 'frame.received';
    at: Date;
    kind: string;
    name: string;
    bytes: number;
    id?: string;
    trace?: Trace;
    family: string;
  };
  'request.started': {
    type: 'request.started';
    at: Date;
    id: string;
    method: string;
    incoming: boolean;
    trace?: Trace;
    family: string;
  };
  'request.ended': {
    type: 'request.ended';
    at: Date;
    id: string;
    method: string;
    incoming: boolean;
    durationMs: number;
    outcome: 'ok' | 'error' | 'cancelled' | 'timeout';
    errorCode?: string;
    trace?: Trace;
    family: string;
  };
  'event.emitted': { type: 'event.emitted'; at: Date; name: string; bytes: number; trace?: Trace; family: string };
  'event.delivered': { type: 'event.delivered'; at: Date; name: string; bytes: number; trace?: Trace; family: string };
  backpressure: { type: 'backpressure'; at: Date; queued: number; stalled: boolean; deadlineMs: number };
  'handler.panic': { type: 'handler.panic'; at: Date; method: string; value: string; trace?: Trace; family: string };
}

/**
 * What a peer tells an observer about the traffic it carries, or what a layer
 * running over a peer tells it. It emits and never aggregates, and chooses no
 * backend: an observer sees names, ids, sizes, durations, outcomes and close
 * codes — never a payload. Diagnostic logging is one observer among others;
 * the runtime writes to no logger.
 *
 * An event that concerns a frame carries that frame's trace and the family of
 * the name it names; a connection or backpressure event concerns no frame and
 * carries neither, stated rather than nullable.
 */
export type ObserverEvent = ObserverEvents[keyof ObserverEvents];

/**
 * One interface, one method; whatever runs over a peer inherits the one it was
 * given. An observer that throws throws alone: the peer catches it, loses that
 * event and carries on, a diagnostic being no reason for a connection to end.
 */
export interface Observer {
  observe(event: ObserverEvent): void;
}

/** The default. A peer given none observes nothing and pays for nothing. */
export const NO_OBSERVER: Observer = Object.freeze({
  observe(): void {
    /* Nothing is aggregated, here or anywhere. */
  },
});
