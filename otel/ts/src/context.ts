import type { Context } from '@opentelemetry/api';
import type { Trace } from '@nightseam/runtime';

/**
 * Which OpenTelemetry context a wire trace stands for, kept beside the trace
 * rather than in it: a `Trace` is the two W3C members the wire carries and
 * gains no member here, so nothing of this reaches a frame, a log line or a
 * consumer's `JSON.stringify` of an event.
 *
 * It is how the two halves of this adapter meet, which they otherwise could
 * not: the propagator is handed a request's context and never the span an
 * observer opened, and an observer is handed an event and never the request's
 * context. Both are handed the one `Trace` object the runtime made for that
 * frame — `traceOf` mints it once per incoming frame and `inject` once per
 * outgoing one — so the mark on it says, to whichever of the two reads it
 * next, which span the frame's trace is.
 *
 * Marked by the observer at an incoming `request.started`, with the server
 * span it just opened, so that a call the handler makes from its context is a
 * child of that span and not a sibling; by `inject`, with the context it
 * injected, so that the client span the observer opens next is parented at the
 * span the frame actually came from; and by `extract` with what the frame
 * itself names, where no observer marked it first.
 */
const contexts = new WeakMap<Trace, Context>();

/** Says which context this trace stands for, and returns the trace. */
export function mark<T extends Trace>(trace: T, context: Context): T {
  contexts.set(trace, context);
  return trace;
}

/** What marked this trace, or nothing where no half of the adapter has yet. */
export function contextOf(trace: Trace | undefined): Context | undefined {
  return trace && contexts.get(trace);
}
