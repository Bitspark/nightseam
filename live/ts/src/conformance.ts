/**
 * The live layer's shared suite: the behavior a scope promises, written once
 * and run by both languages against their own runtime. It is the twin of
 * `live/go/livetest` — the same cases, in the same order, under the same names
 * — and a helper of the repository's tests rather than a published entry point.
 *
 * Every case ends by counting what each scope still holds. A binding nobody
 * released is a leak, and a suite that only compares payloads never sees one.
 */
import { DuplexError } from '@nightseam/runtime';
import {
  CONTRACT_INVALID,
  CONTRACT_MISMATCH,
  REFERENCE_FOREIGN,
  REFERENCE_RELEASED,
  REFERENCE_UNKNOWN,
  SCOPE_CLOSED,
  TOO_MANY_EXPORTS,
  TOO_MANY_IMPORTS,
  forward,
  type Invoke,
  type LiveScope,
  type Reference,
} from './index.ts';

/** What a case reports through: node:test's `t`, or whatever a suite gives it. */
export interface T {
  fail(message: string): void;
}

/** Two scopes over one connection: a and b speak to each other and nothing else. */
export interface Pair {
  a: LiveScope;
  b: LiveScope;
  close: () => void;
}

export interface Case {
  name: string;
  run: (t: T, p: Pair) => Promise<void>;
}

export const SINK = 'probe/Report';
export const JOB = 'probe/Cancel';
export const OTHER = 'probe/SetVolume';

const WAITED = 5000;

/** The callable every case uses where the body does not matter. */
export const echo: Invoke = async (request) => request ?? null;

function deferred<V>(): { promise: Promise<V>; resolve: (value: V) => void } {
  let resolve!: (value: V) => void;
  const promise = new Promise<V>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

async function within<V>(t: T, promise: Promise<V>, what: string): Promise<V> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const expired = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new Error(`${what} never happened`)), WAITED);
  });
  try {
    return await Promise.race([promise, expired]);
  } finally {
    clearTimeout(timer);
  }
}

function refused(t: T, error: unknown, code: string): void {
  if (!(error instanceof DuplexError)) {
    t.fail(`expected the public code ${code}, got ${String(error)}`);
    return;
  }
  if (error.code !== code) t.fail(`expected ${code}, got ${error.code}: ${error.message}`);
}

async function refuses(t: T, run: () => unknown | Promise<unknown>, code: string): Promise<void> {
  try {
    await run();
  } catch (error) {
    refused(t, error, code);
    return;
  }
  t.fail(`expected ${code}, got no refusal at all`);
}

function holds(t: T, scope: LiveScope, exports: number, imports: number, where: string): void {
  const got = scope.counts();
  if (got.exports !== exports || got.imports !== imports) {
    t.fail(
      `${where}: the scope holds ${got.exports} exports and ${got.imports} imports, expected ${exports} and ${imports}`,
    );
  }
}

/**
 * Exports a callable on one side and imports it on the other, the way a payload
 * carrying a reference would: the reference is serialized, crosses, and is
 * decoded by the scope that received it.
 */
export function handed(
  from: LiveScope,
  to: LiveScope,
  contract: string,
  invoke: Invoke,
): { reference: Reference; imported: Invoke } {
  const exported = from.export(contract, invoke);
  const arrived = to.decode(JSON.parse(JSON.stringify(exported)));
  return { reference: arrived, imported: to.import(arrived, contract) };
}

/** The suite, in the order `live/go/livetest` runs it. */
export function cases(): Case[] {
  return [
    { name: 'a callback reaches the side that supplied it', run: callbackAndResult },
    { name: 'a returned callable outlives the call that returned it', run: higherOrder },
    { name: 'two suppliers are told apart', run: independentSuppliers },
    { name: 'one binding imported twice is one attachment', run: aliases },
    { name: 'a reference of another scope is refused', run: foreignReference },
    { name: 'a contract the binding does not carry is refused', run: contractMismatch },
    { name: 'a binding nobody exported is refused', run: unknownReference },
    { name: 'release refuses the next invocation and settles the one in flight', run: releaseIsABarrier },
    { name: 'release invalidates every alias', run: releaseInvalidatesAliases },
    { name: 'cancelling an invocation is not releasing the binding', run: cancellationIsNotRelease },
    { name: 'a scope that closes settles what it had in flight', run: closeSettles },
    {
      name: 'closing the exporter settles a call without closing the peer',
      run: (t, p) => closeImplementation(t, p, false),
    },
    { name: 'closing the scope settles a local self-reference call', run: (t, p) => closeImplementation(t, p, true) },
    { name: 'a reference handed back to its exporter needs no wire', run: selfReference },
    { name: 'forwarding gives the destination its own lifetime', run: forwarding },
    { name: 'a refused export leaves no binding behind', run: boundsLeaveNothing },
  ];
}

async function callbackAndResult(t: T, p: Pair): Promise<void> {
  const reported = deferred<unknown>();
  const { imported } = handed(p.a, p.b, SINK, async (request) => {
    reported.resolve(request);
    return null;
  });
  await imported(50);
  const got = await within(t, reported.promise, 'the callback');
  if (got !== 50) t.fail(`the callback was asked ${String(got)}, expected 50`);
  holds(t, p.a, 1, 0, 'the supplier');
  holds(t, p.b, 0, 1, 'the side it was supplied to');
}

/** The case the whole layer exists for: a supplied callable invoked after the call returned. */
async function higherOrder(t: T, p: Pair): Promise<void> {
  const reported = deferred<unknown>();
  const { imported: progress } = handed(p.a, p.b, SINK, async (request) => {
    reported.resolve(request);
    return null;
  });
  // The exchange that introduced the reference is over; the reference is not.
  const { imported: cancelJob } = handed(p.b, p.a, JOB, async () => progress(100));
  await cancelJob(null);
  const got = await within(t, reported.promise, 'the retained callback');
  if (got !== 100) t.fail(`the retained callback was asked ${String(got)}, expected 100`);
}

async function independentSuppliers(t: T, p: Pair): Promise<void> {
  const first: unknown[] = [];
  const second: unknown[] = [];
  const { imported: one } = handed(p.a, p.b, SINK, async (r) => {
    first.push(r);
    return null;
  });
  const { imported: two } = handed(p.a, p.b, SINK, async (r) => {
    second.push(r);
    return null;
  });
  await one(1);
  await two(2);
  if (first.length !== 1 || first[0] !== 1) t.fail(`the first supplier was asked ${JSON.stringify(first)}`);
  if (second.length !== 1 || second[0] !== 2) t.fail(`the second supplier was asked ${JSON.stringify(second)}`);
  holds(t, p.b, 0, 2, 'two suppliers are two bindings');
}

async function aliases(t: T, p: Pair): Promise<void> {
  const { reference, imported: once } = handed(p.a, p.b, SINK, echo);
  const again = p.b.import(reference, SINK);
  holds(t, p.b, 0, 1, 'one binding imported twice');
  // Both aliases work, and neither takes the other's reply.
  if ((await once(1)) !== 1) t.fail('the first alias got the wrong answer');
  if ((await again(2)) !== 2) t.fail('the second alias got the wrong answer');
}

async function foreignReference(t: T, p: Pair): Promise<void> {
  const exported = p.a.export(SINK, echo);
  // The reference was minted in a's scope; b never decoded it.
  await refuses(t, () => p.b.import(exported, SINK), REFERENCE_FOREIGN);
  holds(t, p.b, 0, 0, 'a foreign reference attaches nothing');
}

async function contractMismatch(t: T, p: Pair): Promise<void> {
  const exported = p.a.export(SINK, echo);
  const arrived = p.b.decode(JSON.parse(JSON.stringify(exported)));
  await refuses(t, () => p.b.import(arrived, OTHER), CONTRACT_MISMATCH);
  holds(t, p.b, 0, 0, 'a mismatched contract attaches nothing');
}

/** The stale token: a binding id of no scope, which is what a reference carried out of its connection becomes. */
async function unknownReference(t: T, p: Pair): Promise<void> {
  const arrived = p.b.decode({ binding: '0000000000000000.1', contract: SINK });
  const invoke = p.b.import(arrived, SINK);
  await refuses(t, () => invoke(null), REFERENCE_UNKNOWN);
}

/** The operator's verdict: the next invocation is refused, the dispatched one settles. */
async function releaseIsABarrier(t: T, p: Pair): Promise<void> {
  const entered = deferred<void>();
  const let_ = deferred<void>();
  const { reference, imported } = handed(p.a, p.b, SINK, async () => {
    entered.resolve();
    await let_.promise;
    return 'settled';
  });

  const inflight = imported(null);
  await within(t, entered.promise, 'the invocation reaching the binding');

  p.b.release(reference);

  // New invocations are refused at once, while the first is still running.
  await refuses(t, () => imported(null), REFERENCE_RELEASED);

  let_.resolve();
  const settled = await within(t, inflight, 'the invocation in flight settling');
  if (settled !== 'settled') t.fail(`the invocation in flight settled with ${String(settled)}`);
  holds(t, p.b, 0, 0, 'the released import');
}

async function releaseInvalidatesAliases(t: T, p: Pair): Promise<void> {
  const { reference, imported: once } = handed(p.a, p.b, SINK, echo);
  const again = p.b.import(reference, SINK);
  p.b.release(reference);
  await refuses(t, () => once(null), REFERENCE_RELEASED);
  await refuses(t, () => again(null), REFERENCE_RELEASED);
  // And importing it again is refused for the reason it was refused for.
  await refuses(t, () => p.b.import(reference, SINK), REFERENCE_RELEASED);
}

/** The three meanings held apart: withdrawing an invocation withdraws that invocation and nothing else. */
async function cancellationIsNotRelease(t: T, p: Pair): Promise<void> {
  const entered = deferred<void>();
  const let_ = deferred<void>();
  const { imported } = handed(p.a, p.b, SINK, async (request) => {
    if (request === 'wait') {
      entered.resolve();
      await let_.promise;
      return 'late';
    }
    return request;
  });

  const withdrawn = new AbortController();
  const pending = imported('wait', { signal: withdrawn.signal });
  const settled = pending.then(
    () => undefined,
    (error: unknown) => error,
  );
  await within(t, entered.promise, 'the invocation reaching the binding');
  withdrawn.abort();
  const outcome = await within(t, settled, 'the withdrawn invocation settling');
  if (outcome === undefined) t.fail('a withdrawn invocation answered');
  let_.resolve();

  // The binding is untouched: cancelling an invocation released nothing.
  if ((await imported('again')) !== 'again') t.fail('the binding did not survive a cancelled invocation');
  holds(t, p.b, 0, 1, 'a cancelled invocation releases no binding');
}

async function closeSettles(t: T, p: Pair): Promise<void> {
  const entered = deferred<void>();
  const let_ = deferred<void>();
  const { imported } = handed(p.a, p.b, SINK, async () => {
    entered.resolve();
    await let_.promise;
    return null;
  });
  const pending = imported(null).then(
    () => undefined,
    (error: unknown) => error,
  );
  await within(t, entered.promise, 'the invocation reaching the binding');
  p.b.close();
  const outcome = await within(t, pending, 'the closed scope settling what it had in flight');
  if (outcome === undefined) t.fail('a scope that closed left an invocation answered');
  let_.resolve();
  holds(t, p.b, 0, 0, 'a closed scope holds nothing');
}

async function closeImplementation(t: T, p: Pair, local: boolean): Promise<void> {
  const entered = deferred<void>();
  const unblock = deferred<void>();
  const finished = deferred<void>();
  const { imported } = handed(p.a, local ? p.a : p.b, SINK, async () => {
    entered.resolve();
    await unblock.promise;
    finished.resolve();
    return 'late';
  });
  p.a.peer.handle('ordinary', async (raw) => raw);
  const pending = imported(null).then(
    () => undefined,
    (error: unknown) => error,
  );
  try {
    await within(t, entered.promise, 'the invocation entering');
    p.a.close();
    const outcome = await within(t, pending, 'closure settling the caller before its implementation');
    refused(t, outcome, SCOPE_CLOSED);
    holds(t, p.a, 0, 0, 'closed exporter');
    const ordinary = async () => {
      const answer = await within(t, p.b.peer.call('ordinary', 'alive'), 'ordinary RPC after live close');
      if (answer !== 'alive') t.fail(`ordinary RPC answered ${String(answer)}`);
    };
    await ordinary();
    unblock.resolve();
    await within(t, finished.promise, 'the unblocked body finishing');
    if ((await pending) !== outcome) t.fail('the late result replaced the closure outcome');
    await ordinary();
    await refuses(t, () => imported(null), SCOPE_CLOSED);
  } finally {
    unblock.resolve();
  }
}

/** A reference of this side's own making, handed back: it reaches the function, not a second dispatch. */
async function selfReference(t: T, p: Pair): Promise<void> {
  let asked = 0;
  const exported = p.a.export(SINK, async (r) => {
    asked += 1;
    return r;
  });
  const back = p.a.decode(JSON.parse(JSON.stringify(exported)));
  const invoke = p.a.import(back, SINK);
  if ((await invoke(7)) !== 7) t.fail('our own binding answered wrongly');
  if (asked !== 1) t.fail(`our own binding was asked ${asked} times`);
  holds(t, p.a, 1, 0, 'our own reference is no import');
}

async function forwarding(t: T, p: Pair): Promise<void> {
  // a exports, b imports, and b forwards it back to a as a binding of its own.
  const { imported } = handed(p.a, p.b, SINK, echo);
  const forwarded = forward(p.b, SINK, imported);
  const arrived = p.a.decode(JSON.parse(JSON.stringify(forwarded)));
  const through = p.a.import(arrived, SINK);
  if ((await through(3)) !== 3) t.fail('the forwarded binding answered wrongly');

  p.b.release(forwarded);
  // The origin is untouched, which is what taking no ownership means.
  if ((await imported(4)) !== 4) t.fail('releasing the forwarded binding disturbed its origin');
}

/** The leak assertion: a refused export registers no binding. */
async function boundsLeaveNothing(t: T, p: Pair): Promise<void> {
  const before = p.a.counts();
  await refuses(t, () => p.a.export('', echo), CONTRACT_INVALID);
  await refuses(t, () => p.a.export(SINK, undefined as unknown as Invoke), CONTRACT_INVALID);
  const after = p.a.counts();
  if (after.exports !== before.exports || after.imports !== before.imports) {
    t.fail(`a refused export left ${JSON.stringify(after)} behind, expected ${JSON.stringify(before)}`);
  }
}

/** The bounds, which each language's own tests reach through the suite's names. */
export const BOUNDS = { TOO_MANY_EXPORTS, TOO_MANY_IMPORTS };
