import assert from 'node:assert/strict';
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { consoleObserver } from './console.ts';
import { DuplexPeer, DuplexError } from './peer.ts';
import type { WebSocketLike } from './peer.ts';
import type { Observer, ObserverEvent } from './observer.ts';

/** Where the lines of each story are kept; `UPDATE_GOLDEN=1 pnpm test` rewrites them. */
const GOLDEN = path.join(import.meta.dirname, '..', 'testdata', 'console');

/** One trace, and a child of it: the two W3C members as a frame carries them. */
const TRACE = { traceparent: '00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01', tracestate: 'bitspark=1' };
const CHILD = { traceparent: '00-4bf92f3577b34da6a3ce929d0e0e4736-b7ad6b7169203331-01' };

/** The instant every story starts at; the clock below moves a millisecond a line. */
const START = Date.parse('2026-09-18T09:00:00.000Z');

/** A string that stands for a payload: it is in every params, result, error and event below. */
const SENTINEL = 'sentinel-6d9f2c-payload';

/** A console that keeps what was written and which method wrote it. */
function capturing() {
  const lines: { method: string; text: string }[] = [];
  const write = (method: string) => (text: string) => { lines.push({ method, text }); };
  return { lines, console: { debug: write('debug'), info: write('info'), warn: write('warn'), error: write('error') } };
}

/** A clock that is fixed and still moves: one millisecond per line, from START. */
function ticking(): () => Date {
  let at = START;
  return () => new Date(at++);
}

/**
 * One story's lines, byte for byte. The events are stated rather than driven,
 * so that a duration and a trace are the same on every host; that a peer emits
 * these events, in this order, is held by the peer's own suite.
 */
function golden(name: string, events: ObserverEvent[]): void {
  const capture = capturing();
  const observer = consoleObserver(capture.console, ticking());
  for (const event of events) observer.observe(event);
  const written = capture.lines.map(line => `${line.text}\n`).join('');
  const file = path.join(GOLDEN, `${name}.log`);
  if (process.env.UPDATE_GOLDEN) writeFileSync(file, written);
  assert.ok(existsSync(file), `no golden ${file}; run with UPDATE_GOLDEN=1`);
  assert.equal(written, readFileSync(file, 'utf8'));
  // Which method a line went through is what the line itself says it did.
  for (const line of capture.lines) assert.ok(line.text.includes(` level=${line.method.toUpperCase()} `), line.text);
}

test('a call a consumer makes, one answered and one refused, is a line each', () => {
  golden('call', [
    { type: 'connection.opened', at: new Date(START), role: 'client' },
    { type: 'request.started', at: new Date(START), id: 'c:1', method: 'work.read', incoming: false, trace: TRACE, family: 'work' },
    { type: 'frame.sent', at: new Date(START), kind: 'request', name: 'work.read', bytes: 96, id: 'c:1', trace: TRACE, family: 'work' },
    { type: 'frame.received', at: new Date(START), kind: 'response', name: 'work.read', bytes: 48, id: 'c:1', trace: TRACE, family: 'work' },
    { type: 'request.ended', at: new Date(START), id: 'c:1', method: 'work.read', incoming: false, durationMs: 7, outcome: 'ok', trace: TRACE, family: 'work' },
    // The second call carries no tracestate, which is then no field of its lines.
    { type: 'request.started', at: new Date(START), id: 'c:2', method: 'work.write', incoming: false, trace: CHILD, family: 'work' },
    { type: 'frame.sent', at: new Date(START), kind: 'request', name: 'work.write', bytes: 104, id: 'c:2', trace: CHILD, family: 'work' },
    { type: 'frame.received', at: new Date(START), kind: 'response', name: 'work.write', bytes: 72, id: 'c:2', trace: CHILD, family: 'work' },
    { type: 'request.ended', at: new Date(START), id: 'c:2', method: 'work.write', incoming: false, durationMs: 12, outcome: 'error', errorCode: 'denied', trace: CHILD, family: 'work' },
  ]);
});

test('a machine serving a call, calling back through it, and a handler that gives up', () => {
  golden('reverse-call', [
    { type: 'frame.received', at: new Date(START), kind: 'request', name: 'work.read', bytes: 96, id: 'c:1', trace: TRACE, family: 'work' },
    { type: 'request.started', at: new Date(START), id: 'c:1', method: 'work.read', incoming: true, trace: TRACE, family: 'work' },
    // The reverse call names a method of no declared family, which is no field.
    { type: 'request.started', at: new Date(START), id: 's:1', method: 'ui.confirm', incoming: false, trace: CHILD, family: '' },
    { type: 'frame.sent', at: new Date(START), kind: 'request', name: 'ui.confirm', bytes: 64, id: 's:1', trace: CHILD, family: '' },
    { type: 'frame.received', at: new Date(START), kind: 'response', name: 'ui.confirm', bytes: 40, id: 's:1', trace: CHILD, family: '' },
    { type: 'request.ended', at: new Date(START), id: 's:1', method: 'ui.confirm', incoming: false, durationMs: 21, outcome: 'ok', trace: CHILD, family: '' },
    { type: 'frame.sent', at: new Date(START), kind: 'response', name: 'work.read', bytes: 48, id: 'c:1', trace: TRACE, family: 'work' },
    { type: 'request.ended', at: new Date(START), id: 'c:1', method: 'work.read', incoming: true, durationMs: 34, outcome: 'ok', trace: TRACE, family: 'work' },
    { type: 'frame.received', at: new Date(START), kind: 'request', name: 'work.boom', bytes: 88, id: 'c:2', trace: TRACE, family: 'work' },
    { type: 'request.started', at: new Date(START), id: 'c:2', method: 'work.boom', incoming: true, trace: TRACE, family: 'work' },
    { type: 'handler.panic', at: new Date(START), method: 'work.boom', value: 'Error: handler failed', trace: TRACE, family: 'work' },
    { type: 'frame.sent', at: new Date(START), kind: 'response', name: 'work.boom', bytes: 64, id: 'c:2', trace: TRACE, family: 'work' },
    { type: 'request.ended', at: new Date(START), id: 'c:2', method: 'work.boom', incoming: true, durationMs: 3, outcome: 'error', errorCode: 'internal', trace: TRACE, family: 'work' },
  ]);
});

test('one event is two lines on the side that emits it and two on the side it reaches', () => {
  golden('event', [
    { type: 'frame.sent', at: new Date(START), kind: 'event', name: 'work.changed', bytes: 52, trace: TRACE, family: 'work' },
    { type: 'event.emitted', at: new Date(START), name: 'work.changed', bytes: 52, trace: TRACE, family: 'work' },
    { type: 'frame.received', at: new Date(START), kind: 'event', name: 'work.changed', bytes: 52, trace: TRACE, family: 'work' },
    { type: 'event.delivered', at: new Date(START), name: 'work.changed', bytes: 52, trace: TRACE, family: 'work' },
  ]);
});

test('a queue that fills, a request that runs out of time, and both ways a connection ends', () => {
  golden('close', [
    { type: 'backpressure', at: new Date(START), queued: 1, stalled: false, deadlineMs: 10_000 },
    { type: 'backpressure', at: new Date(START), queued: 1, stalled: true, deadlineMs: 10_000 },
    { type: 'request.ended', at: new Date(START), id: 'c:3', method: 'work.wait', incoming: false, durationMs: 10_000, outcome: 'timeout', errorCode: 'request_timeout', trace: TRACE, family: 'work' },
    { type: 'connection.closed', at: new Date(START), code: 1011, reason: 'Queue full', local: true },
    // A peer that sent no reason is a line that states it sent none.
    { type: 'connection.closed', at: new Date(START), code: 1006, reason: '', local: false },
  ]);
});

test('an event of a type this adapter does not know is written, at debug, as it stands', () => {
  const capture = capturing();
  const observer = consoleObserver(capture.console, ticking());
  // What #17 opened the registry for: a layer over the peer declares its own.
  observer.observe({ type: 'session.attached', at: new Date(START), role: 'holder', after: 12, trace: TRACE, family: 'session' } as unknown as ObserverEvent);
  assert.deepEqual(capture.lines, [{
    method: 'debug',
    text: 'time=2026-09-18T09:00:00.000Z level=DEBUG msg=session.attached role=holder after=12'
      + ' traceparent=00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01 tracestate="bitspark=1" family=session',
  }]);
});

test('an observer given no clock stamps its lines from the wall clock', () => {
  const capture = capturing();
  const before = Date.now();
  consoleObserver(capture.console).observe({ type: 'connection.opened', at: new Date(0), role: 'client' });
  const stamped = Date.parse(/time=(\S+)/.exec(capture.lines[0].text)![1]);
  assert.ok(stamped >= before && stamped <= Date.now(), capture.lines[0].text);
});

test('no payload reaches a line: not params, not a result, not an error, not an event', async () => {
  const capture = capturing();
  const observer: Observer = consoleObserver(capture.console, ticking());
  const { client, server } = await paired(observer);
  const delivered = deferred();
  client.onEvent('notice', () => { delivered.resolve(); });
  server.handle('read', params => ({ echoed: params, secret: SENTINEL }));
  server.handle('deny', () => { throw new DuplexError('denied', 'Access denied', { secret: SENTINEL }); });
  server.handle('boom', () => { throw new Error('handler failed'); });
  assert.deepEqual(await client.call('read', { secret: SENTINEL }), { echoed: { secret: SENTINEL }, secret: SENTINEL });
  await assert.rejects(client.call('deny', { secret: SENTINEL }), { code: 'denied' });
  await assert.rejects(client.call('boom', { secret: SENTINEL }), { code: 'internal' });
  await server.emit('notice', { secret: SENTINEL });
  await delivered.promise;
  client.close();
  assert.ok(capture.lines.length > 0);
  for (const line of capture.lines) assert.equal(line.text.includes(SENTINEL), false, line.text);
  // The paths that carried it were written ones, not some other traffic.
  const written = capture.lines.map(line => `${line.method} ${line.text}`).join('\n');
  assert.match(written, /warn .*msg=request\.ended .*method=deny .*outcome=error errorCode=denied/);
  assert.match(written, /error .*msg=handler\.panic method=boom value="Error: handler failed"/);
  assert.match(written, /debug .*msg=event\.delivered name=notice/);
  assert.match(written, /info .*msg=connection\.closed code=1000/);
});

/** A socket pair in memory; no WebSocket is involved, as in the peer's own suite. */
class Socket extends EventTarget implements WebSocketLike {
  readyState = 1;
  bufferedAmount = 0;
  partner?: Socket;
  send(text: string): void {
    if (this.readyState !== 1) throw new Error('Closed');
    const partner = this.partner;
    if (partner) queueMicrotask(() => {
      if (partner.readyState === 1) partner.dispatchEvent(new MessageEvent('message', { data: text }));
    });
  }
  close(): void {
    if (this.readyState === 3) return;
    this.readyState = 3;
    this.dispatchEvent(new Event('close'));
    this.partner?.close();
  }
}

async function paired(observer: Observer) {
  const left = new Socket();
  const right = new Socket();
  left.partner = right;
  right.partner = left;
  const client = new DuplexPeer({ observer });
  const server = new DuplexPeer({ role: 'server', observer });
  await Promise.all([client.attach(left), server.attach(right)]);
  return { client, server };
}

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>(accept => { resolve = accept; });
  return { promise, resolve };
}
