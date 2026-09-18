/**
 * The propagator's contract, which is the one thing about this adapter a
 * remote peer can refuse. The runtime does not validate what `inject` returns:
 * a frame whose `traceparent` is anything but the profile's form is refused by
 * the decoder at the other end, so every injection this adapter makes — from
 * no context at all, from a context whose span is not recording, from one
 * whose span context is invalid, from one a text-map propagator writes nothing
 * for and from one it writes nonsense for — is held to that form here.
 */
import assert from 'node:assert/strict';
import test from 'node:test';
import {
  ROOT_CONTEXT, TraceFlags, context as activeContext, trace as traceApi,
  type Context, type TextMapPropagator,
} from '@opentelemetry/api';
import { AsyncLocalStorageContextManager } from '@opentelemetry/context-async-hooks';
import { BasicTracerProvider, InMemorySpanExporter, SimpleSpanProcessor } from '@opentelemetry/sdk-trace-base';
import type { RequestContext, Trace } from '@nightseam/runtime';
import { propagator } from './propagator.ts';

/** The form every peer holds a frame to, from the profile and from #4. */
const FORM = /^[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$/;

/** A trace a frame arrived with. */
const INCOMING: Trace = { traceparent: '00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01', tracestate: 'bitspark=1' };

/** The span context of a span that names nothing: the all-zero ids no peer accepts. */
const INVALID = { traceId: '0'.repeat(32), spanId: '0'.repeat(16), traceFlags: TraceFlags.NONE };

const exporter = new InMemorySpanExporter();
const provider = new BasicTracerProvider({ spanProcessors: [new SimpleSpanProcessor(exporter)] });
const tracer = provider.getTracer('nightseam-otel-test');
activeContext.setGlobalContextManager(new AsyncLocalStorageContextManager().enable());

/** A request context as the runtime hands one to a handler; the peer is never read here. */
function requestContext(trace?: Trace): RequestContext {
  return { signal: new AbortController().signal, requestId: 'c:1', trace } as unknown as RequestContext;
}

/** A context holding a span that is not recording but whose span context is valid. */
function nonRecording(): Context {
  return traceApi.setSpan(ROOT_CONTEXT, traceApi.wrapSpanContext({
    traceId: '4bf92f3577b34da6a3ce929d0e0e4736',
    spanId: '00f067aa0ba902b7',
    traceFlags: TraceFlags.NONE,
  }));
}

/** Every injection of the suite, held to the form before it is looked at further. */
const carried: string[] = [];
function inject(context?: RequestContext, textMap?: TextMapPropagator): Trace {
  const trace = (textMap ? propagator(textMap) : propagator()).inject(context);
  carried.push(trace.traceparent);
  assert.match(trace.traceparent, FORM, 'a frame the remote decoder would refuse');
  assert.equal(trace.tracestate === undefined || typeof trace.tracestate === 'string', true);
  return trace;
}

test('an injection from no context and no active span carries a trace of its own', () => {
  assert.equal(traceApi.getSpan(activeContext.active()), undefined);
  const alone = inject(undefined);
  const empty = inject(requestContext());
  assert.notEqual(alone.traceparent, empty.traceparent);
});

test('an injection from a context whose span context is invalid carries neither of its ids', () => {
  activeContext.with(traceApi.setSpan(ROOT_CONTEXT, traceApi.wrapSpanContext(INVALID)), () => {
    const trace = inject(undefined);
    // The all-zero ids are what the decoder refuses, so a trace of this
    // adapter's own is minted in their place rather than carried.
    assert.notEqual(trace.traceparent.slice(3, 35), '0'.repeat(32));
    assert.notEqual(trace.traceparent.slice(36, 52), '0'.repeat(16));
  });
});

test('an injection from a context whose span is not recording carries it all the same', () => {
  activeContext.with(nonRecording(), () => {
    // A span context that is valid is what the wire wants, recorded or not.
    assert.equal(inject(undefined).traceparent, '00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00');
  });
});

test('an injection from a context whose span is recording carries that span', () => {
  const span = tracer.startSpan('recording');
  activeContext.with(traceApi.setSpan(ROOT_CONTEXT, span), () => {
    const trace = inject(undefined);
    assert.equal(trace.traceparent.slice(3, 35), span.spanContext().traceId);
    assert.equal(trace.traceparent.slice(36, 52), span.spanContext().spanId);
  });
  span.end();
});

test('a text-map propagator that writes nothing, or writes something else, is not what the frame carries', () => {
  const silent: TextMapPropagator = { inject() { /* Writes no member at all. */ }, extract: context => context, fields: () => [] };
  const wrong: TextMapPropagator = {
    inject(_context, carrier) { (carrier as Record<string, string>).traceparent = 'not-a-traceparent'; },
    extract: context => context,
    fields: () => ['traceparent'],
  };
  for (const textMap of [silent, wrong]) {
    // The context names a perfectly good span; what declines to write it, or
    // writes it in some other form, is the text-map propagator.
    const trace = inject(requestContext(INCOMING), textMap);
    assert.notEqual(trace.traceparent, 'not-a-traceparent');
    assert.equal(trace.tracestate, undefined, 'a minted trace continues no vendor state');
  }
});

test('a frame\'s trace reaches the handler\'s context, and the call it makes is a child of it', () => {
  const hook = propagator();
  const context = requestContext();
  hook.extract(context, INCOMING);
  assert.deepEqual(context.trace, INCOMING);
  // No observer opened a span, so what the frame names is the parent, and the
  // call the handler makes belongs to the trace the frame belongs to.
  const outgoing = hook.inject(context);
  assert.equal(outgoing.traceparent.slice(3, 35), '4bf92f3577b34da6a3ce929d0e0e4736');
  assert.equal(outgoing.tracestate, 'bitspark=1');
});

test('a tracestate that arrives without a traceparent continues no trace', () => {
  const hook = propagator();
  const context = requestContext();
  hook.extract(context, { traceparent: '', tracestate: 'bitspark=1' });
  assert.equal(context.trace, undefined);
  hook.extract(context, undefined);
  assert.equal(context.trace, undefined);
  // The call it makes is a trace of its own, and a well-formed one.
  assert.match(hook.inject(context).traceparent, FORM);
});

test('every injection the suite made is one the profile accepts, and no two are the same span', () => {
  assert.equal(carried.length, 7);
  for (const traceparent of carried) assert.match(traceparent, FORM);
  // Each is the span it was made under or a trace minted afresh, so no two of
  // them name one span: a call is never mistaken for the call beside it.
  assert.equal(new Set(carried).size, carried.length);
});
