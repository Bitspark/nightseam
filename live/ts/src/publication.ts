/** Publication outcomes do not choose a consumer's ownership lifetime. */
import { DuplexError, UnpublishedError } from '@nightseam/runtime';
import { REFERENCE_RELEASED, type Invoke, type LiveOwner, type LiveScope } from './index.ts';
import type { Case, Pair, T } from './conformance.ts';

const CONTRACT = 'probe/Report';
const WAITED = 5000;

export function publicationCases(): Case[] {
  return [
    { name: 'retainedAfterTimeout', run: (t, p) => failedSupply(t, p, 'timeout', 1) },
    { name: 'retainedAfterCancellation', run: (t, p) => failedSupply(t, p, 'cancellation', 1) },
    { name: 'retainedAfterRemoteError', run: (t, p) => failedSupply(t, p, 'busy', 1) },
    { name: 'retainedAfterLostReply', run: (t, p) => failedSupply(t, p, 'lost', 1) },
    { name: 'remoteInvokesAfterFailedSupply', run: (t, p) => failedSupply(t, p, 'frame_too_large', 1) },
    { name: 'handlerOwnerAfterLostReply', run: handlerOwnerAfterLostReply },
    { name: 'ownerReleaseWhileRemoteAliasExists', run: (t, p) => failedSupply(t, p, 'refused', 1) },
    { name: 'repeatedFailuresDoNotLeak', run: (t, p) => failedSupply(t, p, 'refused', 12) },
    { name: 'eventPublicationRetainsItsOwner', run: eventPublicationRetainsItsOwner },
    { name: 'scopeClosureEndsRetainedOwners', run: scopeClosureEndsRetainedOwners },
    { name: 'provenUnpublishedIsUnwound', run: provenUnpublishedIsUnwound },
    { name: 'publicationBatchEndsBeforeSend', run: publicationBatchEndsBeforeSend },
    { name: 'localInvocationRetainsNestedRefusal', run: localInvocationRetainsNestedRefusal },
  ];
}

function deferred<V>(): { promise: Promise<V>; resolve: (value: V) => void } {
  let resolve!: (value: V) => void;
  const promise = new Promise<V>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

async function within<V>(promise: Promise<V>, what: string): Promise<V> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([
      promise,
      new Promise<never>((_, reject) => {
        timer = setTimeout(() => reject(new Error(`${what} never completed`)), WAITED);
      }),
    ]);
  } finally {
    clearTimeout(timer);
  }
}

function counts(t: T, value: LiveOwner | LiveScope, exports: number, imports: number, where: string): void {
  const got = value.counts();
  if (got.exports !== exports || got.imports !== imports)
    t.fail(`${where}: holds ${JSON.stringify(got)}, want ${exports} exports and ${imports} imports`);
}

async function waitCounts(scope: LiveScope, exports: number, imports: number): Promise<void> {
  const until = Date.now() + WAITED;
  for (;;) {
    const got = scope.counts();
    if (got.exports === exports && got.imports === imports) return;
    if (Date.now() >= until) throw new Error(`release was not delivered: ${JSON.stringify(got)}`);
    await new Promise((resolve) => setTimeout(resolve, 1));
  }
}

function payload(owner: LiveOwner): unknown {
  return owner.exportValue((build) => build.export(CONTRACT, async (request) => request ?? null).toJSON());
}

function importPayload(owner: LiveOwner, raw: unknown): Invoke {
  return owner.import(owner.scope.decode(raw), CONTRACT);
}

function refused(t: T, error: unknown, code: string): void {
  if (!(error instanceof DuplexError) || error.code !== code) t.fail(`expected ${code}, got ${String(error)}`);
}

function untilAbort(signal: AbortSignal): Promise<void> {
  if (signal.aborted) return Promise.resolve();
  return new Promise((resolve) => signal.addEventListener('abort', () => resolve(), { once: true }));
}

function installAlive(p: Pair): void {
  p.b.peer.handle('publication.alive', async (request) => request);
}

async function alive(t: T, p: Pair): Promise<void> {
  const value = await within(p.a.peer.call('publication.alive', 17), 'ordinary RPC after failed publication');
  if (value !== 17) t.fail(`ordinary RPC returned ${String(value)}`);
}

// The handler imports before refusing. A remote error spelling that is also
// used by local send validation still proves nothing about non-publication.
async function failedSupply(t: T, p: Pair, outcome: string, repetitions: number): Promise<void> {
  installAlive(p);
  const outgoing = p.a.owner().child();
  const incoming = p.b.owner().child();
  let raw: unknown;
  let retained = deferred<Invoke>();
  const unblock = deferred<void>();
  p.b.peer.handle('publication.supply', async (raw, context) => {
    const invoke = importPayload(incoming, raw);
    retained.resolve(invoke);
    if (outcome === 'timeout' || outcome === 'cancellation') {
      await untilAbort(context.signal);
      throw new DuplexError('cancelled', 'The supplying call was withdrawn.');
    }
    if (outcome === 'lost') {
      // A late reply cannot settle the caller that has stopped waiting.
      await unblock.promise;
      return 'late';
    }
    throw new DuplexError(outcome, 'retained before refusing');
  });
  try {
    let alias!: Invoke;
    for (let i = 0; i < repetitions; i++) {
      retained = deferred<Invoke>();
      const controller = new AbortController();
      const done = outgoing
        .publishValue(
          (build) => (raw ??= payload(build)),
          (sent) =>
            p.a.peer.call('publication.supply', sent, {
              signal: controller.signal,
              timeoutMs: outcome === 'timeout' || outcome === 'lost' ? 1000 : WAITED,
            }),
        )
        .then(
          () => undefined,
          (error: unknown) => error,
        );
      alias = await within(retained.promise, 'remote callback retention');
      if (outcome === 'cancellation') controller.abort();
      const error = await within(done, 'failed supplying RPC');
      const expected =
        outcome === 'timeout' || outcome === 'lost'
          ? 'request_timeout'
          : outcome === 'cancellation'
            ? 'cancelled'
            : outcome;
      refused(t, error, expected);
      counts(t, outgoing, 1, 0, 'failed supplier reachable owner');
      counts(t, incoming, 0, 1, 'retaining owner');
      counts(t, p.a, 1, 0, 'supplying scope');
      counts(t, p.b, 0, 1, 'receiving scope');
      if ((await within(alias(41), 'retained callback invocation')) !== 41)
        t.fail(`retained callback failed after ${outcome}`);
      await alive(t, p);
    }
    unblock.resolve();
    outgoing.release();
    await waitCounts(p.b, 0, 0);
    counts(t, outgoing, 0, 0, 'released supplier');
    counts(t, incoming, 0, 0, 'remotely revoked attachment');
    const error = await alias(42).then(
      () => undefined,
      (error: unknown) => error,
    );
    refused(t, error, REFERENCE_RELEASED);
    counts(t, p.a, 0, 0, 'released supplier scope');
    await alive(t, p);
  } finally {
    unblock.resolve();
    outgoing.release();
    incoming.release();
  }
}

async function handlerOwnerAfterLostReply(t: T, p: Pair): Promise<void> {
  installAlive(p);
  const owners = deferred<LiveOwner>();
  const unblock = deferred<void>();
  const returned = deferred<void>();
  p.b.peer.handle('publication.return', async () => {
    // Generated wrappers supply this child in the handler context. The
    // generated socket scenario separately holds that integration boundary.
    const owner = p.b.owner().child();
    const raw = payload(owner);
    owners.resolve(owner);
    await unblock.promise;
    returned.resolve();
    return raw;
  });
  const done = p.a.peer.call('publication.return', undefined, { timeoutMs: 1000 }).then(
    () => undefined,
    (error: unknown) => error,
  );
  let owner: LiveOwner | undefined;
  try {
    owner = await within(owners.promise, 'handler-owned result construction');
    refused(t, await within(done, 'lost reply'), 'request_timeout');
    unblock.resolve();
    await within(returned.promise, 'handler late return');
    counts(t, owner, 1, 0, 'reachable handler owner after lost reply');
    counts(t, p.b, 1, 0, 'handler retained return');
    counts(t, p.a, 0, 0, 'caller received no native value');
    owner.release();
    counts(t, owner, 0, 0, 'released handler owner');
    counts(t, p.b, 0, 0, 'released handler result');
    await alive(t, p);
  } finally {
    unblock.resolve();
    owner?.release();
  }
}

async function eventPublicationRetainsItsOwner(t: T, p: Pair): Promise<void> {
  installAlive(p);
  const outgoing = p.a.owner().child();
  const incoming = p.b.owner().child();
  const retained = deferred<Invoke>();
  const stop = p.b.peer.onEvent('publication.event', (raw) => {
    retained.resolve(importPayload(incoming, raw));
  });
  try {
    await p.a.peer.emit('publication.event', payload(outgoing));
    const alias = await within(retained.promise, 'event receipt');
    counts(t, outgoing, 1, 0, 'event emitter');
    counts(t, incoming, 0, 1, 'event receiver');
    if ((await within(alias(43), 'event callback')) !== 43) t.fail('event callback answered incorrectly');
    outgoing.release();
    await waitCounts(p.b, 0, 0);
    counts(t, p.a, 0, 0, 'event export released');
    await alive(t, p);
  } finally {
    stop();
    outgoing.release();
    incoming.release();
  }
}

async function scopeClosureEndsRetainedOwners(t: T, p: Pair): Promise<void> {
  installAlive(p);
  const owner = p.a.owner().child();
  const child = owner.child();
  payload(child);
  counts(t, child, 1, 0, 'retained child before scope close');
  p.a.close();
  counts(t, owner, 0, 0, 'closed owner');
  counts(t, child, 0, 0, 'closed child');
  counts(t, p.a, 0, 0, 'closed scope');
  await alive(t, p);
}

async function provenUnpublishedIsUnwound(t: T, p: Pair): Promise<void> {
  const owner = p.a.owner().child();
  try {
    for (let i = 0; i < 12; i++) {
      let refused = false;
      try {
        owner.exportValue((build) => {
          build.export(CONTRACT, async (request) => request);
          const cyclic: Record<string, unknown> = {};
          cyclic.self = cyclic;
          return cyclic;
        });
      } catch {
        refused = true;
      }
      if (!refused) t.fail('invalid unpublished payload succeeded');
      const controller = new AbortController();
      controller.abort();
      const callError = await owner
        .publishValue(payload, (raw) => p.a.peer.call('publication.unsent', raw, { signal: controller.signal }))
        .then(
          () => undefined,
          (error: unknown) => error,
        );
      if (!(callError instanceof UnpublishedError)) t.fail('missing local non-delivery proof');
      const eventError = await owner
        .publishValue(payload, (raw) => p.a.peer.emit('', raw))
        .then(
          () => undefined,
          (error: unknown) => error,
        );
      if (!(eventError instanceof UnpublishedError)) t.fail('missing event non-delivery proof');
      counts(t, owner, 0, 0, 'failed construction');
      counts(t, p.a, 0, 0, 'unpublished export unwound');
      counts(t, p.b, 0, 0, 'nothing reached the peer');
    }
  } finally {
    owner.release();
  }
}

async function publicationBatchEndsBeforeSend(t: T, p: Pair): Promise<void> {
  const owner = p.a.owner().child();
  try {
    let captured!: LiveOwner;
    const controller = new AbortController();
    controller.abort();
    const error = await owner
      .publishValue(
        (build) => {
          captured = build;
          return payload(build);
        },
        (raw) => {
          payload(captured);
          return p.a.peer.call('publication.unsent', raw, { signal: controller.signal });
        },
      )
      .then(
        () => undefined,
        (error: unknown) => error,
      );
    if (!(error instanceof UnpublishedError)) t.fail('missing proof');
    counts(t, owner, 1, 0, 'later independent allocation survives');
    counts(t, p.a, 1, 0, 'only original publication was unwound');
  } finally {
    owner.release();
  }
}

async function localInvocationRetainsNestedRefusal(t: T, p: Pair): Promise<void> {
  const implementation = p.a.owner().child();
  const outgoing = p.a.owner().child();
  try {
    let alias!: Invoke;
    const reference = implementation.export('probe/Local', async (raw) => {
      alias = importPayload(implementation, raw);
      const controller = new AbortController();
      controller.abort();
      return p.a.peer.call('publication.nested', undefined, { signal: controller.signal });
    });
    const invoke = implementation.import(reference, 'probe/Local');
    const error = await outgoing
      .publishValue(payload, (raw) => invoke(raw))
      .then(
        () => undefined,
        (error: unknown) => error,
      );
    if (error instanceof UnpublishedError) t.fail('dispatched invocation forwarded nested proof');
    refused(t, error, 'cancelled');
    counts(t, outgoing, 1, 0, 'callback retained after local dispatch');
    if ((await alias(45)) !== 45) t.fail('local retained callback failed');
  } finally {
    implementation.release();
    outgoing.release();
  }
}
