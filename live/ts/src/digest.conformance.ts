import { DuplexError } from '@nightseam/runtime';
import {
  CONTRACT_INVALID,
  CONTRACT_MISMATCH,
  REFERENCE_FOREIGN,
  REFERENCE_RELEASED,
  forward,
  type Invoke,
  type LiveOwner,
  type Reference,
} from './index.ts';
import { echo, SINK, type Case, type Pair, type T } from './conformance.ts';

const ONE = '68025e009ca0e1d178ab57548a507c65dcaefb7b62f3867207b26fdbe5739cb0';
const TWO = 'ccc90bf9332f9595615ec1c0c34627a7cc52779378f7ca00e56d6de7e89955fb';

export function digestCases(): Case[] {
  return [
    { name: 'a reference revision is checked before attachment', run: referenceDigest },
    { name: 'an absent reference digest does not refuse an import', run: absentDigest },
    { name: 'an attachment retains its strongest known digest', run: attachmentDigest },
    { name: 'an exported binding refuses a forged revision', run: exportedDigest },
    { name: 'malformed reference digests allocate nothing', run: malformedDigest },
    { name: 'forwarding preserves a declared digest', run: forwardedDigest },
    { name: 'a digest mismatch unwinds only fresh imports', run: digestRollback },
  ];
}

function refuses(t: T, run: () => unknown, code: string): void {
  try {
    run();
  } catch (error) {
    if (!(error instanceof DuplexError) || error.code !== code) t.fail(`expected ${code}, got ${String(error)}`);
    if (code === CONTRACT_MISMATCH && error instanceof Error && !error.message.includes(SINK))
      t.fail(`mismatch did not name ${SINK}`);
    return;
  }
  t.fail(`expected ${code}, got no refusal`);
}

function exported(t: T, owner: LiveOwner, digest: string, invoke: Invoke = echo): Reference {
  const reference = owner.export(SINK, digest, invoke);
  if (reference.digest !== digest) t.fail('export lost its digest');
  return reference;
}

function holds(t: T, p: Pair, a: number, b: number): void {
  if (
    p.a.counts().exports !== a ||
    p.a.counts().imports !== 0 ||
    p.b.counts().exports !== 0 ||
    p.b.counts().imports !== b
  )
    t.fail(`counts differ: ${JSON.stringify([p.a.counts(), p.b.counts()])}`);
}

function release(t: T, p: Pair): void {
  p.a.owner().release();
  p.b.owner().release();
  holds(t, p, 0, 0);
}

async function referenceDigest(t: T, p: Pair): Promise<void> {
  let called = 0;
  const reference = exported(t, p.a.owner(), ONE, async (raw) => {
    called++;
    return raw;
  });
  const arrived = p.b.decode(reference.toJSON());
  refuses(t, () => p.b.owner().import(arrived, SINK, TWO), CONTRACT_MISMATCH);
  holds(t, p, 1, 0);
  if (called !== 0) t.fail('refused import invoked the implementation');
  const invoke = p.b.owner().import(arrived, SINK, ONE);
  if ((await invoke(41)) !== 41) t.fail('same revision did not invoke');
  release(t, p);
}

async function absentDigest(t: T, p: Pair): Promise<void> {
  for (const [sent, expected] of [
    ['', ONE],
    [ONE, ''],
    ['', ''],
  ]) {
    const reference = exported(t, p.a.owner(), sent!);
    if (Object.hasOwn(reference.toJSON(), 'digest') !== (sent !== '')) t.fail('absent digest was emitted');
    const invoke = p.b.owner().import(p.b.decode(reference.toJSON()), SINK, expected!);
    await invoke(42);
  }
  release(t, p);
}

async function attachmentDigest(t: T, p: Pair): Promise<void> {
  const reference = exported(t, p.a.owner(), '');
  const arrived = p.b.decode(reference.toJSON());
  const first = p.b.owner().import(arrived, SINK, '');
  const known = p.b.decode({ ...reference.toJSON(), digest: ONE });
  if (p.b.owner().import(known, SINK, '') !== first) t.fail('narrowing replaced its attachment');
  if (arrived.digest !== '') t.fail('narrowing rewrote the received reference');
  if (p.b.owner().import(arrived, SINK, '') !== first) t.fail('absent alias replaced its attachment');
  const conflict = p.b.decode({ ...reference.toJSON(), digest: TWO });
  refuses(t, () => p.b.owner().import(conflict, SINK, ''), CONTRACT_MISMATCH);
  refuses(t, () => p.b.owner().import(arrived, SINK, TWO), CONTRACT_MISMATCH);
  holds(t, p, 1, 1);
  const second = p.b.decode(exported(t, p.a.owner(), '').toJSON());
  p.b.owner().import(second, SINK, ONE);
  refuses(t, () => p.b.owner().import(second, SINK, TWO), CONTRACT_MISMATCH);
  release(t, p);
}

async function exportedDigest(t: T, p: Pair): Promise<void> {
  const reference = exported(t, p.a.owner(), ONE);
  const forged = p.a.decode({ ...reference.toJSON(), digest: TWO });
  refuses(t, () => p.a.owner().import(forged, SINK, TWO), CONTRACT_MISMATCH);
  refuses(t, () => p.a.owner().import(forged, SINK, ''), CONTRACT_MISMATCH);
  refuses(t, () => p.b.owner().import(reference, SINK, ONE), REFERENCE_FOREIGN);
  holds(t, p, 1, 0);
  release(t, p);
}

async function malformedDigest(t: T, p: Pair): Promise<void> {
  for (const invalid of ['short', 'A'.repeat(64), 'g'.repeat(64), ONE + '0', ONE + '\n']) {
    refuses(t, () => p.a.owner().export(SINK, invalid, echo), CONTRACT_INVALID);
    const reference = p.b.decode({ binding: 'other.1', contract: SINK });
    refuses(t, () => p.b.owner().import(reference, SINK, invalid), CONTRACT_INVALID);
  }
  for (const digest of ['', null, true, 7, [], 'short', 'A'.repeat(64), ONE + '\n']) {
    refuses(t, () => p.b.decode({ binding: 'other.1', contract: SINK, digest }), CONTRACT_INVALID);
  }
  holds(t, p, 0, 0);
}

async function forwardedDigest(t: T, p: Pair): Promise<void> {
  const reference = exported(t, p.a.owner(), ONE);
  const origin = p.b.owner().import(p.b.decode(reference.toJSON()), SINK, ONE);
  const forwarded = forward(p.b.owner(), SINK, ONE, origin);
  if (forwarded.digest !== ONE) t.fail('forward lost its digest');
  const through = p.a.owner().import(p.a.decode(forwarded.toJSON()), SINK, ONE);
  await through(43);
  release(t, p);
}

async function digestRollback(t: T, p: Pair): Promise<void> {
  const owner = p.b.owner().child();
  const prior = p.b.decode(exported(t, p.a.owner(), ONE).toJSON());
  const freshRef = p.b.decode(exported(t, p.a.owner(), ONE).toJSON());
  const wrong = p.b.decode(exported(t, p.a.owner(), TWO).toJSON());
  const retained = owner.import(prior, SINK, ONE);
  let fresh: Invoke | undefined;
  refuses(
    t,
    () =>
      owner.importValue((batch) => {
        fresh = batch.import(freshRef, SINK, ONE);
        batch.import(wrong, SINK, ONE);
      }),
    CONTRACT_MISMATCH,
  );
  if (p.b.counts().imports !== 1) t.fail('digest rollback lost the prior import or kept its fresh import');
  await retained(44);
  if (!fresh) {
    t.fail('batch did not reach its fresh import');
    return;
  }
  try {
    await fresh(45);
    t.fail('fresh import survived rollback');
  } catch (error) {
    if (!(error instanceof DuplexError) || error.code !== REFERENCE_RELEASED)
      t.fail(`rollback refusal: ${String(error)}`);
  }
  release(t, p);
}
