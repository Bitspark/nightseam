import type { Observer, ObserverEvent } from './observer.ts';
import type { Trace } from './trace.ts';

/**
 * The one shipped TypeScript adapter of the Observer: diagnostic logging, the
 * twin of the Go `slog` one. Each event becomes one line, written through the
 * console method its kind deserves, and the line carries names, ids, sizes,
 * durations, outcomes and close codes — never a payload, which reaches no
 * observer by any path and so reaches no line here.
 *
 * The line is the Go adapter's, so that the two read alike:
 *
 * ```
 * time=2026-09-18T09:00:00.000Z level=WARN msg=request.ended id=c:2 method=work.write incoming=false durationMs=12 outcome=error errorCode=denied family=work
 * ```
 *
 * No dependency: `@nightseam/runtime` takes none, and a console is what every
 * host already has.
 */

/** The level a line says, which is the console method it is written through. */
type Level = 'debug' | 'info' | 'warn' | 'error';

/**
 * The fields each event of the runtime is written with, in the one order a
 * line writes them — the order the event declares, with `trace` standing for
 * the two W3C members it holds. A type absent from this table is one a layer
 * over the peer added, the tunnel's or the session's or a later profile's, and
 * is written from its own keys rather than dropped.
 */
const FIELDS: Readonly<Record<string, readonly string[]>> = {
  'connection.opened': ['role'],
  'connection.closed': ['code', 'reason', 'local'],
  'frame.sent': ['kind', 'name', 'bytes', 'id', 'trace', 'family'],
  'frame.received': ['kind', 'name', 'bytes', 'id', 'trace', 'family'],
  'request.started': ['id', 'method', 'incoming', 'trace', 'family'],
  'request.ended': ['id', 'method', 'incoming', 'durationMs', 'outcome', 'errorCode', 'trace', 'family'],
  'event.emitted': ['name', 'bytes', 'trace', 'family'],
  'event.delivered': ['name', 'bytes', 'trace', 'family'],
  backpressure: ['queued', 'stalled', 'deadlineMs'],
  'handler.panic': ['method', 'value', 'trace', 'family'],
};

/** The level per kind; what is not named here is debug, the traffic's own level. */
const LEVELS: Readonly<Record<string, Level>> = {
  'connection.opened': 'info',
  'connection.closed': 'info',
  'handler.panic': 'error',
};

/** The three fields an empty value is an absence of; every other is written empty. */
const WHEN_PRESENT: ReadonlySet<string> = new Set(['traceparent', 'tracestate', 'family']);

/**
 * An observer that writes each event as one line. The console is the four
 * methods it writes through, so that a capture in a test is four functions and
 * not a Console; the clock stamps the line, which is written as the event is
 * observed and so at the instant the event itself carries.
 */
export function consoleObserver(
  console: Pick<Console, 'debug' | 'info' | 'warn' | 'error'> = globalThis.console,
  clock: () => Date = () => new Date(),
): Observer {
  return {
    observe(event: ObserverEvent): void {
      // Read as a record rather than as the union: an event of a type this
      // adapter does not know is one a layer above it added, and is written.
      const fields = event as unknown as Record<string, unknown>;
      const level = levelOf(fields);
      console[level]([`time=${clock().toISOString()}`, `level=${level.toUpperCase()}`, ...pairs(fields)].join(' '));
    },
  };
}

/**
 * What a line is written at. A request the application refused is what a
 * reader of a log is looking for; a cancellation and a deadline are outcomes
 * of their own, as the runtime states them, and are no louder than the frames
 * they rode on.
 */
function levelOf(fields: Record<string, unknown>): Level {
  if (fields.type === 'request.ended') return fields.outcome === 'error' ? 'warn' : 'debug';
  return LEVELS[fields.type as string] ?? 'debug';
}

/** One event as its line's pairs: what it is, then its fields in the fixed order. */
function* pairs(fields: Record<string, unknown>): Generator<string> {
  const type = typeof fields.type === 'string' ? fields.type : '';
  yield `msg=${render(type)}`;
  // `at` is no field of the line: the line's own time is when it was observed.
  const keys = FIELDS[type] ?? Object.keys(fields).filter(key => key !== 'type' && key !== 'at');
  for (const key of keys) {
    if (key === 'trace') {
      const trace = fields.trace as Trace | undefined;
      yield* pair('traceparent', trace?.traceparent);
      yield* pair('tracestate', trace?.tracestate);
      continue;
    }
    yield* pair(key, fields[key]);
  }
}

/**
 * A field, or nothing. A field an event does not carry is no field of its
 * line; a trace member and a family are carried or not, and an empty one is
 * the not, as the Go adapter has it. Every other field a line states even
 * where it is empty, a reason of none reading `reason=""`.
 */
function* pair(key: string, value: unknown): Generator<string> {
  if (value === undefined || value === null) return;
  if (value === '' && WHEN_PRESENT.has(key)) return;
  yield `${key}=${render(value)}`;
}

/** A value as a line carries it; what a line could not carry bare it quotes. */
function render(value: unknown): string {
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  if (value instanceof Date) return value.toISOString();
  const text = typeof value === 'string' ? value : json(value);
  return quoting(text) ? JSON.stringify(text) : text;
}

/**
 * What a reader could not split back out of a line, and so what is quoted:
 * slog's rule — nothing at all, a space or any other blank, a quote, an
 * equals, a character below one, or the one above the printable range.
 */
function quoting(text: string): boolean {
  if (text === '') return true;
  for (const character of text) {
    const code = character.codePointAt(0) ?? 0;
    if (code <= 0x20 || code === 0x7f || character === '"' || character === '=' || /\s/.test(character)) return true;
  }
  return false;
}

/** A field of a layer's own event that is neither scalar nor date, as it stands. */
function json(value: unknown): string {
  try { return JSON.stringify(value) ?? String(value); } catch { return String(value); }
}
