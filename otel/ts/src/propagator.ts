import {
  ROOT_CONTEXT,
  context as activeContext,
  defaultTextMapGetter,
  defaultTextMapSetter,
  type TextMapPropagator,
} from '@opentelemetry/api';
import { W3CTraceContextPropagator } from '@opentelemetry/core';
import type { Propagator, Trace } from '@nightseam/runtime';
import { contextOf, mark } from './context.ts';
import { TRACEPARENT, carrierOf, minted } from './wire.ts';

/**
 * The runtime's propagator hook over an OpenTelemetry text-map propagator: an
 * incoming frame's trace becomes the remote span context a handler's calls are
 * children of, and an outgoing frame carries the span the call was made under.
 * `W3CTraceContextPropagator` is the default and the profile's own form, so a
 * consumer replaces it only to add a vendor's members beside the two standard
 * ones.
 *
 * What an outgoing frame carries is **always** a `traceparent` of the form the
 * profile accepts, `^[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$`. The
 * runtime does not validate what a propagator injects and the remote decoder
 * refuses a frame whose member is anything else, so an injection that a
 * text-map propagator declines to write — no span in the context, a span
 * context that is invalid, tracing suppressed — or writes in some other form
 * mints a trace of this adapter's own rather than returning nothing. The span
 * an observer opens for such a frame belongs to that minted trace, so a call
 * made outside every span is still one trace end to end.
 *
 * `context.active()` is what an injection from no request context reads, which
 * is the root unless the application registered a context manager; a consumer
 * that wants its own spans to be the parents of the calls made under them
 * registers one, as it would for any other instrumentation.
 */
export function propagator(textMap: TextMapPropagator = new W3CTraceContextPropagator()): Propagator {
  return {
    extract(context, trace) {
      // A tracestate that arrives without a traceparent continues no trace, as
      // the specification says; the response to its frame still carries it back.
      if (!trace?.traceparent) return;
      context.trace = trace;
      // The observer marks an incoming request's trace with the server span it
      // opened, a moment before this runs, so that what the handler calls is a
      // child of the handler's own span. Where there is no observer, what the
      // frame names is the parent, and the call is a sibling of the frame.
      if (!contextOf(trace)) mark(trace, textMap.extract(ROOT_CONTEXT, carrierOf(trace), defaultTextMapGetter));
    },
    inject(context) {
      const parent = contextOf(context?.trace) ?? activeContext.active();
      const carrier: Record<string, string> = {};
      textMap.inject(parent, carrier, defaultTextMapSetter);
      if (!TRACEPARENT.test(carrier.traceparent ?? '')) {
        const fresh = minted();
        return mark(fresh.trace, fresh.context);
      }
      const trace: Trace = { traceparent: carrier.traceparent! };
      if (carrier.tracestate) trace.tracestate = carrier.tracestate;
      return mark(trace, parent);
    },
  };
}
