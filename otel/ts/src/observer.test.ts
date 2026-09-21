/**
 * What the observer makes of each event, stated rather than driven, so that
 * what a backend is read in does not depend on the host: requests, connections
 * and application events have spans, other events annotate their enclosing span, and an attribute
 * is one of the event's own scalar fields and nothing else. That a peer emits
 * these events in this order is held by the peer's own suite; what one call
 * through a relay makes of them is held beside this, in `spans.test.ts`.
 */
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { SpanKind, SpanStatusCode, type Span, type Tracer } from '@opentelemetry/api';
import {
  BasicTracerProvider,
  AlwaysOffSampler,
  InMemorySpanExporter,
  SimpleSpanProcessor,
  type ReadableSpan,
} from '@opentelemetry/sdk-trace-base';
import type { Observer, ObserverEvent } from '@nightseam/runtime';
import { EVENTS } from './observer.check.ts';
import { observer } from './observer.ts';

/** A string that stands for a payload; no event of any layer declares a member one could arrive in. */
const SENTINEL = 'sentinel-6d9f2c-payload';
/** The instant every stated event carries, and the trace of one frame. */
const at = new Date('2026-09-18T09:00:00.000Z');
const TRACE = { traceparent: '00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01', tracestate: 'bitspark=1' };

/**
 * One peer's observer over a tracer that keeps what it opened, so that a test
 * can put the span id of an open span in the trace of an event, which is what
 * a frame of that exchange would carry.
 */
function watching(onStart?: () => void) {
  const exporter = new InMemorySpanExporter();
  const provider = new BasicTracerProvider({
    spanProcessors: [
      new SimpleSpanProcessor(exporter),
      {
        onStart(_span, _parent) {
          onStart?.();
        },
        onEnd() {},
        async forceFlush() {},
        async shutdown() {},
      },
    ],
  });
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
    tell: (...events: ObserverEvent[]) => {
      for (const event of events) told.observe(event);
    },
    /** The trace a frame of the span last opened would carry. */
    of: () => ({ traceparent: `00-4bf92f3577b34da6a3ce929d0e0e4736-${last}-01` }),
  };
}

/** One span's events as names, which is what a reader of a trace sees first. */
const names = (span: ReadableSpan) => span.events.map((event) => event.name);

interface SequenceSpan {
  name: string;
  kind: string;
  start_ms: number;
  end_ms: number;
  trace_id?: string;
  parent_span_id: string;
  attributes?: Record<string, unknown>;
  events: { name: string; at_ms: number }[];
}

const sequences = JSON.parse(
  readFileSync(new URL('../../../conformance/tables/otel-events.json', import.meta.url), 'utf8'),
) as {
  cases: {
    name: string;
    events: (Record<string, unknown> & { at_ms: number })[];
    on_start?: (Record<string, unknown> & { at_ms: number })[];
    spans: SequenceSpan[];
  }[];
};

test('the shared observer fixture has cases', () => assert.ok(sequences.cases.length > 0));
for (const row of sequences.cases) {
  test(`shared observer sequence: ${row.name}`, () => {
    const origin = Date.parse('2026-09-21T00:00:00Z');
    let first = true;
    const watch = watching(() => {
      if (!first) return;
      first = false;
      for (const { at_ms, ...event } of row.on_start ?? [])
        watch.tell({ ...event, at: new Date(origin + at_ms) } as ObserverEvent);
    });
    for (const { at_ms, ...event } of row.events)
      watch.tell({ ...event, at: new Date(origin + at_ms) } as ObserverEvent);
    const spans = watch.spans();
    assert.equal(spans.length, row.spans.length, spans.map((span) => span.name).join(', '));
    const milliseconds = ([seconds, nanos]: [number, number]) => seconds * 1000 + nanos / 1e6 - origin;
    for (const [i, expected] of row.spans.entries()) {
      const span = spans[i]!;
      assert.equal(span.name, expected.name);
      assert.equal(SpanKind[span.kind]!.toLowerCase(), expected.kind);
      assert.equal(milliseconds(span.startTime), expected.start_ms);
      assert.equal(milliseconds(span.endTime), expected.end_ms);
      assert.equal(span.parentSpanContext?.spanId ?? '', expected.parent_span_id);
      if (expected.trace_id) assert.equal(span.spanContext().traceId, expected.trace_id);
      for (const [key, value] of Object.entries(expected.attributes ?? {}))
        assert.deepEqual(span.attributes[key], value, `${span.name}: ${key}`);
      assert.deepEqual(
        span.events.map((event) => ({ name: event.name, at_ms: milliseconds(event.time) })),
        expected.events,
      );
    }
  });
}

test('an incoming request is a server span and an outgoing one a client span, named for the method', () => {
  const watch = watching();
  watch.tell(
    { type: 'request.started', at, id: 'c:1', method: 'work.read', incoming: true, trace: TRACE, family: 'work' },
    { type: 'request.started', at, id: 's:1', method: 'ui.confirm', incoming: false, family: '' },
    {
      type: 'request.ended',
      at: new Date(at.getTime() + 21),
      id: 's:1',
      method: 'ui.confirm',
      incoming: false,
      durationMs: 21,
      outcome: 'ok',
      family: '',
    },
    {
      type: 'request.ended',
      at: new Date(at.getTime() + 34),
      id: 'c:1',
      method: 'work.read',
      incoming: true,
      durationMs: 34,
      outcome: 'ok',
      trace: TRACE,
      family: 'work',
    },
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
    'nightseam.id': 'c:1',
    'nightseam.method': 'work.read',
    'nightseam.incoming': true,
    'nightseam.family': 'work',
    'nightseam.durationMs': 34,
    'nightseam.outcome': 'ok',
  });
  // A request whose frame carried no trace at all begins a trace of its own.
  assert.notEqual(confirm!.spanContext().traceId, read!.spanContext().traceId);
});

test('how a request ended is the span status, and the error code is an attribute of it', () => {
  const watch = watching();
  for (const [id, outcome, errorCode] of [
    ['c:1', 'ok', undefined],
    ['c:2', 'error', 'denied'],
    ['c:3', 'cancelled', undefined],
    ['c:4', 'timeout', 'request_timeout'],
  ] as const) {
    watch.tell(
      { type: 'request.started', at, id, method: 'work.write', incoming: false, family: 'work' },
      {
        type: 'request.ended',
        at,
        id,
        method: 'work.write',
        incoming: false,
        durationMs: 1,
        outcome,
        errorCode,
        family: 'work',
      },
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

test('an application event keeps its frame parent and never annotates the request instead', () => {
  const watch = watching();
  watch.tell({ type: 'connection.opened', at, role: 'client' });
  watch.tell({
    type: 'request.started',
    at,
    id: 'c:1',
    method: 'work.read',
    incoming: false,
    trace: TRACE,
    family: 'work',
  });
  watch.tell(
    // By the request: every frame of an exchange says which request it is.
    { type: 'frame.sent', at, kind: 'request', name: 'work.read', bytes: 96, id: 'c:1', trace: TRACE, family: 'work' },
    // An application event gets its own moment under the parent its frame names.
    { type: 'event.emitted', at, name: 'work.changed', bytes: 52, trace: watch.of(), family: 'work' },
    // Closing the connection does not add an annotation to the open request.
    { type: 'connection.closed', at, code: 1011, reason: 'Queue full', local: true },
    // There is no connection left to enclose this unrelated frame.
    { type: 'frame.received', at, kind: 'response', name: 'other.read', bytes: 48, id: 'c:9', family: 'other' },
    {
      type: 'event.delivered',
      at,
      name: 'other.changed',
      bytes: 12,
      trace: { traceparent: '00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000009-01' },
      family: 'other',
    },
  );
  watch.tell({
    type: 'request.ended',
    at,
    id: 'c:1',
    method: 'work.read',
    incoming: false,
    durationMs: 7,
    outcome: 'ok',
    trace: TRACE,
    family: 'work',
  });
  const [emitted, connection, delivered, span] = watch.spans();
  assert.deepEqual(names(span!), ['frame.sent']);
  assert.equal(emitted!.kind, SpanKind.PRODUCER);
  assert.equal(emitted!.parentSpanContext?.spanId, span!.spanContext().spanId);
  assert.equal(delivered!.kind, SpanKind.CONSUMER);
  assert.equal(delivered!.parentSpanContext?.spanId, '0000000000000009');
  assert.deepEqual(connection!.attributes, {
    'nightseam.role': 'client',
    'nightseam.close.code': 1011,
    'nightseam.close.reason': 'Queue full',
    'nightseam.close.local': true,
  });
  assert.deepEqual(connection!.events, []);
});

test('the tunnel reaches the connection span while a request is open', () => {
  const watch = watching();
  watch.tell({ type: 'connection.opened', at, role: 'server' });
  watch.tell({
    type: 'request.started',
    at,
    id: 'c:1',
    method: 'channel.open',
    incoming: true,
    trace: TRACE,
    family: '',
  });
  // A channel's events name no request and carry no trace: they are the
  // connection's, even while a channel.open request is in flight.
  watch.tell(EVENTS.find((event) => event.type === 'channel.accepted')!);
  watch.tell({ type: 'credit.stall', at, family: 'probe', id: 3, waiting: 2 });
  watch.tell({
    type: 'request.ended',
    at,
    id: 'c:1',
    method: 'channel.open',
    incoming: true,
    durationMs: 1,
    outcome: 'ok',
    trace: TRACE,
    family: '',
  });
  watch.tell({ type: 'connection.closed', at, code: 1000, reason: '', local: true });
  const [span, connection] = watch.spans();
  assert.deepEqual(names(span!), []);
  assert.deepEqual(names(connection!), ['channel.accepted', 'credit.stall']);
  assert.deepEqual(connection!.events[1]!.attributes, {
    'nightseam.family': 'probe',
    'nightseam.id': 3,
    'nightseam.waiting': 2,
  });
});

test('no payload reaches a span: every event of every layer, and a structure smuggled into each', () => {
  const started: ObserverEvent = {
    type: 'request.started',
    at,
    id: 'c:1',
    method: 'work.read',
    incoming: false,
    trace: TRACE,
    family: 'work',
  };
  const spans: ReadableSpan[] = [];
  for (const event of EVENTS) {
    // Each event as the layer declares it, and each carrying what no layer
    // declares: a structure is the one shape a payload could arrive in, and
    // it is the shape no attribute is ever written from.
    const smuggled = {
      ...event,
      params: { secret: SENTINEL },
      data: [SENTINEL],
    } as unknown as ObserverEvent;
    for (const told of [event, smuggled]) {
      const watch = watching();
      if (told.type !== 'connection.opened') watch.tell({ type: 'connection.opened', at, role: 'client' });
      if (told.type !== 'request.started') watch.tell(started);
      watch.tell(told);
      watch.tell({
        type: 'request.ended',
        at,
        id: 'c:1',
        method: 'work.read',
        incoming: false,
        durationMs: 7,
        outcome: 'ok',
        trace: TRACE,
        family: 'work',
      });
      watch.tell({ type: 'connection.closed', at, code: 1000, reason: '', local: true });
      spans.push(...watch.spans());
    }
  }
  assert.equal(spans.length > 0, true);
  for (const span of spans) {
    assert.equal(JSON.stringify(span.attributes).includes(SENTINEL), false, span.name);
    assert.equal(JSON.stringify(span.events).includes(SENTINEL), false, span.name);
    // What a span carries it could only have read off a field of an event:
    // under the one prefix, and in one of the three shapes.
    for (const written of [span.attributes, ...span.events.map((event) => event.attributes ?? {})]) {
      for (const [key, value] of Object.entries(written)) {
        assert.equal(key.startsWith('nightseam.'), true, key);
        assert.equal(['string', 'number', 'boolean'].includes(typeof value), true, `${key} is ${typeof value}`);
      }
    }
  }
});

test('connection and application event spans obey the supplied tracer sampler', async () => {
  const exporter = new InMemorySpanExporter();
  const provider = new BasicTracerProvider({
    sampler: new AlwaysOffSampler(),
    spanProcessors: [new SimpleSpanProcessor(exporter)],
  });
  try {
    const told = observer(provider.getTracer('consumer-sampling'));
    for (const event of EVENTS) told.observe(event);
    assert.deepEqual(exporter.getFinishedSpans(), []);
  } finally {
    await provider.shutdown();
  }
});

test('a consumer span processor can observe the next connection while the previous span ends', async () => {
  const exporter = new InMemorySpanExporter();
  let told: Observer;
  let first = true;
  const reopened = new Date(at.getTime() + 10);
  const provider = new BasicTracerProvider({
    spanProcessors: [
      new SimpleSpanProcessor(exporter),
      {
        onStart() {},
        onEnd() {
          if (first) {
            first = false;
            told.observe({ type: 'connection.opened', at: reopened, role: 'server' });
          }
        },
        async forceFlush() {},
        async shutdown() {},
      },
    ],
  });
  try {
    told = observer(provider.getTracer('consumer-processor'));
    told.observe({ type: 'connection.opened', at, role: 'client' });
    told.observe({ type: 'connection.closed', at, code: 1000, reason: '', local: true });
    told.observe({ type: 'connection.closed', at: reopened, code: 1001, reason: '', local: false });
    assert.deepEqual(
      exporter
        .getFinishedSpans()
        .map((span) => [span.attributes['nightseam.role'], span.attributes['nightseam.close.code']]),
      [
        ['client', 1000],
        ['server', 1001],
      ],
    );
  } finally {
    await provider.shutdown();
  }
});
