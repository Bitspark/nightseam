import { ROOT_CONTEXT, TraceFlags, trace as traceApi, defaultTextMapGetter, type Context } from '@opentelemetry/api';
import { W3CTraceContextPropagator } from '@opentelemetry/core';
import type { Trace } from '@nightseam/runtime';

/**
 * The one form the profile accepts and every peer holds a frame to:
 * `version-traceid-spanid-flags`, lower-case hexadecimal. The runtime does not
 * validate what a propagator injects, so this adapter validates it here, on the
 * way out, rather than leaving a frame for the remote decoder to refuse.
 */
export const TRACEPARENT = /^[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$/;

/**
 * The wire is W3C by profile, so what a frame carries is read as W3C whatever
 * text-map propagator a consumer chose for the other direction: the members are
 * the standard's, and reading them any other way would read a frame the peer
 * accepted as one it did not.
 */
const w3c = new W3CTraceContextPropagator();

/** A frame's trace as a context holding the remote span it names, or the root where it names none. */
export function wireContext(trace: Trace | undefined): Context {
  if (!trace?.traceparent) return ROOT_CONTEXT;
  return w3c.extract(ROOT_CONTEXT, carrierOf(trace), defaultTextMapGetter);
}

/** The two members as a carrier, which is the shape every text-map propagator reads and writes. */
export function carrierOf(trace: Trace): Record<string, string> {
  const carrier: Record<string, string> = { traceparent: trace.traceparent };
  if (trace.tracestate) carrier.tracestate = trace.tracestate;
  return carrier;
}

/** The span id a trace names, which is what an event of that trace is enclosed by. */
export function spanIdOf(trace: Trace | undefined): string | undefined {
  return trace?.traceparent && TRACEPARENT.test(trace.traceparent) ? trace.traceparent.slice(36, 52) : undefined;
}

/**
 * A trace of this adapter's own, sampled, with the context it stands for: what
 * an injection falls back to where there is no span to carry. The context holds
 * the ids the wire will carry and not the root, so that the span an observer
 * opens for the frame belongs to the trace the frame belongs to.
 */
export function minted(): { trace: Trace; context: Context } {
  const traceId = randomHex(16);
  const spanId = randomHex(8);
  return {
    trace: { traceparent: `00-${traceId}-${spanId}-01` },
    context: traceApi.setSpanContext(ROOT_CONTEXT, { traceId, spanId, traceFlags: TraceFlags.SAMPLED, isRemote: false }),
  };
}

/** Web Crypto is the only source, as it is in the runtime's own default. */
function randomHex(bytes: number): string {
  return Array.from(crypto.getRandomValues(new Uint8Array(bytes)), byte => byte.toString(16).padStart(2, '0')).join('');
}
