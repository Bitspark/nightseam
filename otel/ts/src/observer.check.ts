/**
 * What this package is held to by `pnpm check`: the adapter compiles against
 * the whole of the runtime's `ObserverEvents` registry as the layers above it
 * leave it — the runtime's ten events, the tunnel's five and the session's ten
 * — and every one of the twenty-five below narrows to the fields declared for
 * it, so a field renamed a layer away is a compile error here rather than an
 * attribute that quietly stops being written.
 *
 * The adapter itself declares nothing into that registry and switches over
 * nothing in it: a request becomes a span and everything else becomes a span
 * event, read off the event's own fields, so a twenty-sixth event of a layer
 * added later is recorded without this package knowing its name. These are
 * spelled out all the same, because what an event is called and what it
 * carries is what a backend will be read in, and the suite takes the same list
 * to hold what reaches a span and what never does.
 *
 * It is no part of what the package ships: `tsconfig.build.json` excludes it.
 */
import { trace as traceApi } from '@opentelemetry/api';
import type { ObserverEvent } from '@nightseam/runtime';
import '@nightseam/tunnel';
import '@nightseam/session';
import { observer } from './index.ts';

/** The instant every event below carries; the suite gives them their own. */
const at = new Date('2026-09-18T09:00:00.000Z');
/** One trace, as a frame carries it. */
const trace = { traceparent: '00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01', tracestate: 'bitspark=1' };

/** One of every event the three layers declare, in the order they declare them. */
export const EVENTS: ObserverEvent[] = [
  { type: 'connection.opened', at, role: 'client' },
  { type: 'connection.closed', at, code: 1000, reason: 'done', local: true },
  { type: 'frame.sent', at, kind: 'request', name: 'work.read', bytes: 96, id: 'c:1', trace, family: 'work' },
  { type: 'frame.received', at, kind: 'response', name: 'work.read', bytes: 48, id: 'c:1', trace, family: 'work' },
  { type: 'request.started', at, id: 'c:1', method: 'work.read', incoming: false, trace, family: 'work' },
  {
    type: 'request.ended',
    at,
    id: 'c:1',
    method: 'work.read',
    incoming: false,
    durationMs: 7,
    outcome: 'error',
    errorCode: 'denied',
    trace,
    family: 'work',
  },
  { type: 'event.emitted', at, name: 'work.changed', bytes: 52, trace, family: 'work' },
  { type: 'event.delivered', at, name: 'work.changed', bytes: 52, trace, family: 'work' },
  { type: 'backpressure', at, queued: 1, stalled: true, deadlineMs: 10_000 },
  { type: 'handler.panic', at, method: 'work.boom', value: 'Error: handler failed', trace, family: 'work' },
  { type: 'channel.opened', at, family: 'probe', id: 3, after: 0, opener: true },
  { type: 'channel.accepted', at, family: 'probe', id: 3, after: 0 },
  { type: 'channel.closed', at, family: 'probe', id: 3, code: 1000, reason: 'done' },
  { type: 'credit.stall', at, family: 'probe', id: 3, waiting: 2 },
  { type: 'open.refused', at, family: 'probe', reason: 'no capacity' },
  { type: 'session.bound', at, session: 's' },
  { type: 'session.unbound', at, session: 's', code: 4002, reason: 'the machine went away' },
  { type: 'session.attached', at, session: 's', role: 'participant', origin: 'one', after: 0 },
  { type: 'session.detached', at, session: 's', role: 'participant', origin: 'one' },
  { type: 'ask.raised', at, session: 's', id: 's:1', method: 'reverse', asking: true, trace },
  { type: 'ask.routed', at, session: 's', id: 's:1', method: 'reverse', origin: 'one', trace },
  { type: 'ask.answered', at, session: 's', id: 's:1', method: 'reverse', origin: 'one', trace },
  { type: 'control.changed', at, session: 's', origin: 'one', held: true },
  {
    type: 'frame.appended',
    at,
    session: 's',
    sequence: 1,
    direction: 'up',
    origin: 'one',
    bytes: 96,
    method: 'echo',
    trace,
  },
  {
    type: 'session.refused',
    at,
    session: 's',
    code: 'not_controlling',
    method: 'echo',
    role: 'participant',
    origin: 'two',
    trace,
  },
];

/** The adapter takes every one of them, as the peer of a consumer of all three layers would hand it. */
export function feed(): void {
  const told = observer(traceApi.getTracer('nightseam'));
  for (const event of EVENTS) told.observe(event);
}
