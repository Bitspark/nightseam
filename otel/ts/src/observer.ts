import {
  ROOT_CONTEXT,
  SpanKind,
  SpanStatusCode,
  trace as traceApi,
  type Attributes,
  type Span,
  type Tracer,
} from '@opentelemetry/api';
import type { Observer, ObserverEvent } from '@nightseam/runtime';
import { contextOf, mark } from './context.ts';
import { wireContext } from './wire.ts';

/**
 * The runtime's observer over an OpenTelemetry tracer: a request becomes a
 * span, a server one where the request came in and a client one where it went
 * out. The connection has its own span from open to close, and an application
 * event is a zero-duration producer or consumer span under its frame's trace.
 *
 * An observer is a peer's, so one of these belongs to one peer: the spans it
 * has open are that peer's requests. A frame is recorded on the unique open
 * request its id names, or on the connection when it names none or is
 * ambiguous. Other runtime and layer events belong to the connection alone.
 * Without an enclosing span there is nowhere to record a span event.
 *
 * What a span carries is the event's own fields, under `nightseam.`, and only
 * the ones that are a string, a number or a boolean — never a payload, which
 * reaches no observer by any path, and never a structure, which is the one
 * shape a payload could arrive in from a layer declared after this adapter.
 */
export function observer(tracer: Tracer): Observer {
  /** Incoming and outgoing requests can bear the same id. */
  const byRequest = new Map<string, Span>();
  const requestKey = (id: string, incoming: boolean) => `${incoming ? 'in' : 'out'}:${id}`;
  let connection: Span | undefined;

  return {
    observe(event: ObserverEvent): void {
      const fields = event as unknown as Record<string, unknown>;
      const at = event.at;
      switch (event.type) {
        case 'connection.opened':
          connection = tracer.startSpan(
            'connection',
            { kind: SpanKind.INTERNAL, attributes: { 'nightseam.role': event.role }, startTime: at },
            ROOT_CONTEXT,
          );
          return;
        case 'connection.closed': {
          const closed = connection;
          connection = undefined;
          if (closed) {
            closed.setAttributes({
              'nightseam.close.code': event.code,
              'nightseam.close.reason': event.reason,
              'nightseam.close.local': event.local,
            });
            closed.end(at);
          }
          return;
        }
        case 'event.emitted':
        case 'event.delivered': {
          const span = tracer.startSpan(
            event.name,
            {
              kind: event.type === 'event.emitted' ? SpanKind.PRODUCER : SpanKind.CONSUMER,
              attributes: {
                'nightseam.name': event.name,
                'nightseam.bytes': event.bytes,
                ...(event.family ? { 'nightseam.family': event.family } : {}),
              },
              startTime: at,
            },
            wireContext(event.trace),
          );
          span.end(at);
          return;
        }
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
          byRequest.set(requestKey(event.id, event.incoming), span);
          // What the handler calls is a child of the handler's span: the
          // propagator reads this mark off the request context it is given.
          if (event.incoming && event.trace) mark(event.trace, traceApi.setSpan(parent, span));
          return;
        }
        case 'request.ended': {
          const key = requestKey(event.id, event.incoming);
          const span = byRequest.get(key);
          if (!span) return;
          byRequest.delete(key);
          span.setAttributes(attributes(fields));
          span.setStatus(
            event.outcome === 'ok'
              ? { code: SpanStatusCode.OK }
              : { code: SpanStatusCode.ERROR, message: event.errorCode ?? event.outcome },
          );
          span.end(at);
          return;
        }
        default: {
          const span =
            event.type === 'frame.sent' || event.type === 'frame.received' ? enclosing(event.id) : connection;
          span?.addEvent(event.type, attributes(fields), at);
        }
      }
    },
  };

  /** A frame belongs to one open request, or to the connection as a whole. */
  function enclosing(id: string | undefined): Span | undefined {
    if (id) {
      const incoming = byRequest.get(requestKey(id, true));
      const outgoing = byRequest.get(requestKey(id, false));
      if (Boolean(incoming) !== Boolean(outgoing)) return incoming ?? outgoing;
    }
    return connection;
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
