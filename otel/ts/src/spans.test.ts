/**
 * The span tree of one call, end to end: a consumer calls a method of the
 * probe family through a session relay, the machine's handler asks the holder
 * of control to reverse what it was given, and the holder answers. The
 * scenario is the session component's own — its `pipes`, its governance, its
 * generated client — run with this adapter on every peer of it, and what it
 * is held to is what #2 §1 lists: one trace from the consumer's call to the
 * holder's answer, each hop's spans children of the span that caused them.
 *
 * Each hop is two spans and not one: the runtime asks a propagator what an
 * outgoing frame carries before it tells an observer the request began, so
 * what a frame names is the span the call was made under, and the client span
 * of the call and the server span of the handler that serves it are both
 * children of it. The nesting is therefore between hops, which is where a
 * reader of a trace looks for it.
 */
import assert from 'node:assert/strict';
import test from 'node:test';
import { SpanKind, context as activeContext, type Tracer } from '@opentelemetry/api';
import { AsyncLocalStorageContextManager } from '@opentelemetry/context-async-hooks';
import { BasicTracerProvider, InMemorySpanExporter, SimpleSpanProcessor, type ReadableSpan } from '@opentelemetry/sdk-trace-base';
import { DuplexPeer } from '@nightseam/runtime';
import { memoryLog, Registry } from '@nightseam/session';
import { Client, type Handler, type Payload } from '../../../cmd/nightseam/testdata/golden/api/ts/probe-client/src/index.ts';
import { governance, pipes } from '@nightseam/session/conformance';
import { observer } from './observer.ts';
import { propagator } from './propagator.ts';

/** A string that stands for a payload: it is in the params, the result and the answer below. */
const SENTINEL = 'sentinel-6d9f2c-payload';
/** The family's names, as the generated client labels the peer it makes. */
const FAMILIES = { echo: 'probe', no_args: 'probe', seen: 'probe', reverse: 'probe', changed: 'probe', noticed: 'probe' };
const tick = () => new Promise(resolve => setTimeout(resolve, 5));

// An injection from outside a request reads the active context, which means
// what the application's own spans mean only where a context manager keeps it.
activeContext.setGlobalContextManager(new AsyncLocalStorageContextManager().enable());

/** A tracer over an exporter that keeps what ended, as a consumer's own would be. */
function recording() {
  const exporter = new InMemorySpanExporter();
  const provider = new BasicTracerProvider({ spanProcessors: [new SimpleSpanProcessor(exporter)] });
  return { provider, tracer: provider.getTracer('nightseam-otel-test'), spans: () => exporter.getFinishedSpans() };
}

/**
 * One call, from a consumer attached to a session to the machine bound to it
 * and back through the ask the handler raises: the adapter is the propagator
 * and the observer of every peer that speaks the family, and the relay's own
 * peer observes through it too, so nothing of the run is outside the trace.
 */
async function calling(tracer: Tracer, text: string): Promise<Payload> {
  const wire = await pipes(observer(tracer));
  const registry = new Registry();
  const { near: machine, far: up } = await wire.open();
  registry.bind('s', up, governance, memoryLog(1 << 20));

  // The machine: it serves echo, and inside that handler it asks whoever holds
  // control to reverse what it was given, from the handler's own context.
  const served = new DuplexPeer({ role: 'server', propagator: propagator(), observer: observer(tracer), families: FAMILIES });
  served.handle('echo', async (params, context) => {
    const reversed = await served.call<Payload>('reverse', params, { context });
    return { ...reversed, text: 'machine:' + reversed.text };
  });
  await served.attach(machine);

  // The consumer: attached as a participant, holding control, answering asks.
  const consumer = await wire.open(0);
  registry.control('s', registry.attach('s', consumer.far, 'participant', 'one', 0));
  const answering: Handler = { reverse: params => ({ ...params, text: [...params.text].reverse().join('') }) };
  const client = await Client.attach(consumer.near, { propagator: propagator(), observer: observer(tracer) }, answering);

  // The consumer's own span, which is what the call is made under: it is the
  // one span of the trace this adapter did not open.
  const answer = await tracer.startActiveSpan('consumer.call', async root => {
    try { return await client.echo({ text, count: 1 }); } finally { root.end(); }
  });
  await tick();
  wire.close();
  await tick();
  return answer;
}

/** The spans of the trace one span belongs to, which is the call's and not the tunnel's. */
function of(spans: ReadableSpan[], name: string): ReadableSpan[] {
  const root = spans.find(span => span.name === name);
  assert.ok(root, `no span named ${name} in ${spans.map(span => span.name).join(', ')}`);
  return spans.filter(span => span.spanContext().traceId === root.spanContext().traceId);
}

test('one call through a relay to a handler whose ask is answered is one trace, parented hop by hop', async () => {
  const kept = recording();
  assert.deepEqual(await calling(kept.tracer, 'value'), { text: 'machine:eulav', count: 1 });
  await kept.provider.forceFlush();
  const spans = of(kept.spans(), 'consumer.call');
  const named = (name: string, kind: SpanKind) => {
    const found = spans.filter(span => span.name === name && span.kind === kind);
    assert.equal(found.length, 1, `${found.length} spans named ${name} of kind ${kind}`);
    return found[0]!;
  };
  const root = named('consumer.call', SpanKind.INTERNAL);
  const called = named('echo', SpanKind.CLIENT);
  const served = named('echo', SpanKind.SERVER);
  const asked = named('reverse', SpanKind.CLIENT);
  const answered = named('reverse', SpanKind.SERVER);
  // The call the consumer made and the handler the relay carried it to are
  // both of the span the consumer made it under.
  assert.equal(called.parentSpanContext?.spanId, root.spanContext().spanId);
  assert.equal(served.parentSpanContext?.spanId, root.spanContext().spanId);
  // What the handler asked, and the holder that answered it, are both of the
  // handler's own span: the relay re-minted the id and forwarded the trace.
  assert.equal(asked.parentSpanContext?.spanId, served.spanContext().spanId);
  assert.equal(answered.parentSpanContext?.spanId, served.spanContext().spanId);
  // The two a frame crossed a process to reach are parented remotely, from
  // what the frame named; the ask is parented at the span itself, which the
  // handler's own context carried to it and which names no remoteness.
  assert.equal(served.parentSpanContext?.isRemote, true);
  assert.equal(answered.parentSpanContext?.isRemote, true);
  assert.equal(asked.parentSpanContext?.isRemote, undefined);
  // Nothing else of the call is in the trace, and every span of it ended.
  assert.deepEqual(spans.length, 5);
  for (const span of spans) assert.equal(span.ended, true, span.name);
  // Each request's frames are events of its span, and the family is on it.
  // The session's own vocabulary travels the same connection, so a cursor
  // delivered while the call stood open is on the call's span too: every
  // event the peer delivered is a frame received and a delivery, and the one
  // frame received that is neither is the answer the call waited for.
  const events = called.events.map(event => event.name);
  assert.equal(events[0], 'frame.sent');
  assert.equal(events.filter(name => name === 'frame.received').length,
    events.filter(name => name === 'event.delivered').length + 1);
  assert.equal(served.attributes['nightseam.family'], 'probe');
  assert.equal(served.attributes['nightseam.incoming'], true);
  assert.equal(asked.attributes['nightseam.method'], 'reverse');
});

test('no payload reaches a span of a call that carried one, anywhere in the trace', async () => {
  const kept = recording();
  const answer = await calling(kept.tracer, SENTINEL);
  assert.equal(answer.text.includes([...SENTINEL].reverse().join('')), true);
  await kept.provider.forceFlush();
  const spans = kept.spans();
  assert.equal(spans.length > 0, true);
  // Every span of the run, the tunnel's own among them, and not only the
  // call's: what a span is named, what it carries and what happened in it.
  for (const span of spans) {
    const written = JSON.stringify({ name: span.name, attributes: span.attributes, events: span.events, status: span.status });
    assert.equal(written.includes(SENTINEL), false, span.name);
  }
});
