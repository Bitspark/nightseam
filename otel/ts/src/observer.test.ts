/**
 * What the observer makes of each event, stated rather than driven, so that
 * what a backend is read in does not depend on the host: a request is a span,
 * everything else is a span event on the span it belongs to, and an attribute
 * is one of the event's own scalar fields and nothing else. That a peer emits
 * these events in this order is held by the peer's own suite; what one call
 * through a relay makes of them is held beside this, in `spans.test.ts`.
 */
import assert from 'node:assert/strict';
import test from 'node:test';
import { SpanKind, SpanStatusCode, type Span, type Tracer } from '@opentelemetry/api';
import { BasicTracerProvider, InMemorySpanExporter, SimpleSpanProcessor, type ReadableSpan } from '@opentelemetry/sdk-trace-base';
import type { ObserverEvent } from '@nightseam/runtime';
import { EVENTS } from './observer.check.ts';
import { observer } from './observer.ts';

/** A string that stands for a payload; no event of the three layers declares a member one could arrive in. */
const SENTINEL = 'sentinel-6d9f2c-payload';
/** The instant every stated event carries, and the trace of one frame. */
const at = new Date('2026-09-18T09:00:00.000Z');
const TRACE = { traceparent: '00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01', tracestate: 'bitspark=1' };

/**
 * One peer's observer over a tracer that keeps what it opened, so that a test
 * can put the span id of an open span in the trace of an event, which is what
 * a frame of that exchange would carry.
 */
function watching() {
  const exporter = new InMemorySpanExporter();
  const provider = new BasicTracerProvider({ spanProcessors: [new SimpleSpanProcessor(exporter)] });
  const under = provider.getTracer('nightseam-otel-test');
  let last = '';
  const tracer: Tracer = {
    startSpan(...args: Parameters<Tracer['startSpan']>): Span {
      const span = under.startSpan(...args);
      last = span.spanContext().spanId;
      return span;
    },
    startActiveSpan: under.startActiveSpan.bind(under) as Tracer['startActiveSpan'],
  };
  const told = observer(tracer);
  return {
    spans: () => exporter.getFinishedSpans(),
    tell: (...events: ObserverEvent[]) => { for (const event of events) told.observe(event); },
    /** The trace a frame of the span last opened would carry. */
    of: () => ({ traceparent: `00-4bf92f3577b34da6a3ce929d0e0e4736-${last}-01` }),
  };
}

/** One span's events as names, which is what a reader of a trace sees first. */
const names = (span: ReadableSpan) => span.events.map(event => event.name);

test('an incoming request is a server span and an outgoing one a client span, named for the method', () => {
  const watch = watching();
  watch.tell(
    { type: 'request.started', at, id: 'c:1', method: 'work.read', incoming: true, trace: TRACE, family: 'work' },
    { type: 'request.started', at, id: 's:1', method: 'ui.confirm', incoming: false, family: '' },
    { type: 'request.ended', at: new Date(at.getTime() + 21), id: 's:1', method: 'ui.confirm', incoming: false, durationMs: 21, outcome: 'ok', family: '' },
    { type: 'request.ended', at: new Date(at.getTime() + 34), id: 'c:1', method: 'work.read', incoming: true, durationMs: 34, outcome: 'ok', trace: TRACE, family: 'work' },
  );
  const [confirm, read] = watch.spans();
  assert.equal(read!.name, 'work.read');
  assert.equal(read!.kind, SpanKind.SERVER);
  assert.equal(confirm!.name, 'ui.confirm');
  assert.equal(confirm!.kind, SpanKind.CLIENT);
  // A server span belongs to the trace the frame belongs to, under the span
  // the frame named, which is a span of somebody else's process.
  assert.equal(read!.spanContext().traceId, '4bf92f3577b34da6a3ce929d0e0e4736');
  assert.equal(read!.parentSpanContext?.spanId, '00f067aa0ba902b7');
  assert.equal(read!.parentSpanContext?.isRemote, true);
  // The attributes are the fields of both events under one prefix, and the
  // trace is none of them: the span context already says what it would say.
  assert.deepEqual(read!.attributes, {
    'nightseam.id': 'c:1', 'nightseam.method': 'work.read', 'nightseam.incoming': true, 'nightseam.family': 'work',
    'nightseam.durationMs': 34, 'nightseam.outcome': 'ok',
  });
  // A request whose frame carried no trace at all begins a trace of its own.
  assert.notEqual(confirm!.spanContext().traceId, read!.spanContext().traceId);
});

test('how a request ended is the span status, and the error code is an attribute of it', () => {
  const watch = watching();
  for (const [id, outcome, errorCode] of [['c:1', 'ok', undefined], ['c:2', 'error', 'denied'], ['c:3', 'cancelled', undefined], ['c:4', 'timeout', 'request_timeout']] as const) {
    watch.tell(
      { type: 'request.started', at, id, method: 'work.write', incoming: false, family: 'work' },
      { type: 'request.ended', at, id, method: 'work.write', incoming: false, durationMs: 1, outcome, errorCode, family: 'work' },
    );
  }
  const [ok, denied, cancelled, timeout] = watch.spans();
  assert.deepEqual(ok!.status, { code: SpanStatusCode.OK });
  assert.deepEqual(denied!.status, { code: SpanStatusCode.ERROR, message: 'denied' });
  assert.equal(denied!.attributes['nightseam.errorCode'], 'denied');
  // A cancellation and a deadline name no error code of their own, so what
  // the outcome was is what the status says in its place.
  assert.deepEqual(cancelled!.status, { code: SpanStatusCode.ERROR, message: 'cancelled' });
  assert.deepEqual(timeout!.status, { code: SpanStatusCode.ERROR, message: 'request_timeout' });
});

test('an event names its span by the request it belongs to, or by the trace it carries, or by neither', () => {
  const watch = watching();
  watch.tell({ type: 'request.started', at, id: 'c:1', method: 'work.read', incoming: false, trace: TRACE, family: 'work' });
  watch.tell(
    // By the request: every frame of an exchange says which request it is.
    { type: 'frame.sent', at, kind: 'request', name: 'work.read', bytes: 96, id: 'c:1', trace: TRACE, family: 'work' },
    // By the trace: an event names no request, and the span it belongs to is
    // the one the frame's trace names, which is the span just opened.
    { type: 'event.emitted', at, name: 'work.changed', bytes: 52, trace: watch.of(), family: 'work' },
    // By neither: what names no request and carries no trace concerns the
    // whole connection, and is recorded on every span it still has open.
    { type: 'connection.closed', at, code: 1011, reason: 'Queue full', local: true },
    // And what names a request, or the trace of a span, this peer does not
    // have is recorded where that span is, which is not here.
    { type: 'frame.received', at, kind: 'response', name: 'other.read', bytes: 48, id: 'c:9', family: 'other' },
    { type: 'event.delivered', at, name: 'other.changed', bytes: 12, trace: { traceparent: '00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000009-01' }, family: 'other' },
  );
  watch.tell({ type: 'request.ended', at, id: 'c:1', method: 'work.read', incoming: false, durationMs: 7, outcome: 'ok', trace: TRACE, family: 'work' });
  const [span] = watch.spans();
  assert.deepEqual(names(span!), ['frame.sent', 'event.emitted', 'connection.closed']);
  assert.deepEqual(span!.events[2]!.attributes, { 'nightseam.code': 1011, 'nightseam.reason': 'Queue full', 'nightseam.local': true });
});

test('the tunnel and the session reach a span by the same two rules as the runtime', () => {
  const watch = watching();
  watch.tell({ type: 'request.started', at, id: 'c:1', method: 'channel.open', incoming: true, trace: TRACE, family: '' });
  // A channel's events name no request and carry no trace: they are the
  // connection's, and are recorded on what it has open, as a close is.
  watch.tell(EVENTS.find(event => event.type === 'channel.accepted')!);
  // A session's event carries the trace of the frame it concerns, so it is
  // recorded where that frame's span is and nowhere else.
  watch.tell({ type: 'ask.raised', at, session: 's', id: 'of no peer', method: 'reverse', asking: true, trace: TRACE });
  watch.tell({ type: 'frame.appended', at, session: 's', sequence: 1, direction: 'up', origin: 'one', bytes: 96, method: 'echo', trace: watch.of() });
  watch.tell({ type: 'request.ended', at, id: 'c:1', method: 'channel.open', incoming: true, durationMs: 1, outcome: 'ok', trace: TRACE, family: '' });
  const [span] = watch.spans();
  assert.deepEqual(names(span!), ['channel.accepted', 'frame.appended']);
  assert.deepEqual(span!.events[1]!.attributes, {
    'nightseam.session': 's', 'nightseam.sequence': 1, 'nightseam.direction': 'up',
    'nightseam.origin': 'one', 'nightseam.bytes': 96, 'nightseam.method': 'echo',
  });
});

test('no payload reaches a span: every event of the three layers, and a structure smuggled into each', () => {
  const watch = watching();
  watch.tell({ type: 'request.started', at, id: 'c:1', method: 'work.read', incoming: false, trace: TRACE, family: 'work' });
  const enclosing = watch.of();
  for (const event of EVENTS) {
    // Each event as the layer declares it, and each carrying what no layer
    // declares: a structure is the one shape a payload could arrive in, and
    // it is the shape no attribute is ever written from.
    const smuggled = { ...event, trace: enclosing, params: { secret: SENTINEL }, data: [SENTINEL] } as unknown as ObserverEvent;
    watch.tell({ ...event, trace: enclosing } as ObserverEvent, smuggled);
  }
  watch.tell({ type: 'request.ended', at, id: 'c:1', method: 'work.read', incoming: false, durationMs: 7, outcome: 'ok', trace: TRACE, family: 'work' });
  const spans = watch.spans();
  assert.equal(spans.length > 0, true);
  for (const span of spans) {
    assert.equal(JSON.stringify(span.attributes).includes(SENTINEL), false, span.name);
    assert.equal(JSON.stringify(span.events).includes(SENTINEL), false, span.name);
    // What a span carries it could only have read off a field of an event:
    // under the one prefix, and in one of the three shapes.
    for (const written of [span.attributes, ...span.events.map(event => event.attributes ?? {})]) {
      for (const [key, value] of Object.entries(written)) {
        assert.equal(key.startsWith('nightseam.'), true, key);
        assert.equal(['string', 'number', 'boolean'].includes(typeof value), true, `${key} is ${typeof value}`);
      }
    }
  }
});
