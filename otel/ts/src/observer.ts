import {
  SpanKind,
  SpanStatusCode,
  trace as traceApi,
  type Attributes,
  type Span,
  type Tracer,
} from '@opentelemetry/api';
import type { Observer, ObserverEvent, Trace } from '@nightseam/runtime';
import { contextOf, mark } from './context.ts';
import { spanIdOf, wireContext } from './wire.ts';

/**
 * The runtime's observer over an OpenTelemetry tracer: a request becomes a
 * span, a server one where the request came in and a client one where it went
 * out, and every other event becomes a span event on the span it belongs to.
 *
 * An observer is a peer's, so one of these belongs to one peer: the spans it
 * has open are that peer's requests, and an event of a layer running over that
 * peer — a tunnel's — is recorded on whichever of them encloses
 * it. An event names its request by the id the profile correlates it
 * under where it has one and by the trace it carries otherwise; an event that
 * names neither — a channel's id is a number and names no request — concerns
 * the whole connection,
 * `connection.closed` with its code among them, and is recorded on every span
 * the connection still has open.
 *
 * What a span carries is the event's own fields, under `nightseam.`, and only
 * the ones that are a string, a number or a boolean — never a payload, which
 * reaches no observer by any path, and never a structure, which is the one
 * shape a payload could arrive in from a layer declared after this adapter.
 */
export function observer(tracer: Tracer): Observer {
  /** The peer's open requests, by the id the profile correlates them under. */
  const byRequest = new Map<string, Span>();
  /** The same spans by their own span id, which is what a frame's trace names. */
  const bySpan = new Map<string, Span>();

  return {
    observe(event: ObserverEvent): void {
      const fields = event as unknown as Record<string, unknown>;
      const at = event.at;
      switch (event.type) {
        case 'request.started': {
          // Outgoing, the trace was marked by the injection that minted it, so
          // the parent is the span the call was made under; incoming, nothing
          // has marked it yet and the parent is what the frame names.
          const parent = contextOf(event.trace) ?? wireContext(event.trace);
          const span = tracer.startSpan(
            event.method,
            {
              kind: event.incoming ? SpanKind.SERVER : SpanKind.CLIENT,
              attributes: attributes(fields),
              startTime: at,
            },
            parent,
          );
          byRequest.set(event.id, span);
          bySpan.set(span.spanContext().spanId, span);
          // What the handler calls is a child of the handler's span: the
          // propagator reads this mark off the request context it is given.
          if (event.incoming && event.trace) mark(event.trace, traceApi.setSpan(parent, span));
          return;
        }
        case 'request.ended': {
          const span = byRequest.get(event.id);
          if (!span) return;
          span.setAttributes(attributes(fields));
          span.setStatus(
            event.outcome === 'ok'
              ? { code: SpanStatusCode.OK }
              : { code: SpanStatusCode.ERROR, message: event.errorCode ?? event.outcome },
          );
          span.end(at);
          byRequest.delete(event.id);
          bySpan.delete(span.spanContext().spanId);
          return;
        }
        default: {
          const recorded = attributes(fields);
          for (const span of enclosing(fields)) span.addEvent(event.type, recorded, at);
        }
      }
    },
  };

  /** The spans an event belongs to: the one it names, or all of them where it names none. */
  function enclosing(fields: Record<string, unknown>): Iterable<Span> {
    // A request id is the profile's, a string; a channel's id is a number and
    // names no request, which is why what is read here is the string one.
    const id = typeof fields.id === 'string' ? fields.id : undefined;
    const named = id === undefined ? undefined : byRequest.get(id);
    if (named) return [named];
    const spanId = spanIdOf(fields.trace as Trace | undefined);
    const traced = spanId ? bySpan.get(spanId) : undefined;
    if (traced) return [traced];
    // An event that names a request or a trace this peer has no span for is a
    // frame of somebody else's span, and is recorded where that span is.
    if (id !== undefined || fields.trace !== undefined) return [];
    return byRequest.values();
  }
}

/**
 * An event's fields as a span's attributes. `type` is the span's name or its
 * event's, `at` is its time, and a field that is neither a string nor a number
 * nor a boolean — the trace, whose members the span context already is, and
 * whatever a layer declared after this adapter carries — is no attribute at
 * all, so that nothing structured can reach a backend through this.
 */
function attributes(fields: Record<string, unknown>): Attributes {
  const carried: Attributes = {};
  for (const [key, value] of Object.entries(fields)) {
    if (key === 'type' || key === 'at') continue;
    if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean')
      carried[`nightseam.${key}`] = value;
  }
  return carried;
}
