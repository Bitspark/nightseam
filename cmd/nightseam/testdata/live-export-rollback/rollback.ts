import assert from 'node:assert/strict';
import { pipe } from '@nightseam/duplex';
import { DuplexPeer, DuplexError } from '@nightseam/runtime';
import { liveOver, TOO_MANY_EXPORTS } from '@nightseam/live';
import * as protocol from './api/ts/rollback-client/src/types.ts';

const [a, b] = pipe();
const pa = new DuplexPeer({ role: 'client' }),
  pb = new DuplexPeer({ role: 'server' });
const sa = liveOver(pa, { maxExports: 2 }),
  sb = liveOver(pb, { maxExports: 4 });
await Promise.all([pa.attach(a), pb.attach(b)]);
try {
  const fn: protocol.Call = async () => 7;
  const baseline = protocol.exportCall(sa.owner(), fn);
  const retained = protocol.importCall(sb.owner(), baseline);
  let used = false;
  const use = protocol.importUse(
    sa.owner(),
    protocol.exportUse(sb.owner(), async () => {
      used = true;
      return 0;
    }),
  );
  const makePair = protocol.importMake(
    sa.owner(),
    protocol.exportMake(sb.owner(), async () => ({ first: fn, second: fn })),
  );
  const check = protocol.importCheck(
    sa.owner(),
    protocol.exportCheck(sb.owner(), async () => {
      used = true;
    }),
  );
  const before = sa.counts(),
    remoteBefore = sb.counts();
  const cyclic: Record<string, unknown> = {};
  cyclic.self = cyclic;
  for (const name of [
    'record',
    'union',
    'generic',
    'generic-live',
    'serialization',
    'validation',
    'request',
    'reply',
  ]) {
    for (let i = 0; i < 3; i++) {
      await assert.rejects(
        async () => {
          switch (name) {
            case 'record':
              return protocol.exportPairUnchecked(sa.owner(), { first: fn, second: fn });
            case 'union':
              return protocol.exportChoiceUnchecked(sa.owner(), { kind: 'pair', value: { first: fn, second: fn } });
            case 'generic':
              return protocol.exportGenericUnchecked(sa.owner(), [{ item: fn }, { item: fn }]);
            case 'generic-live':
              return protocol.exportBoundUnchecked(sa.owner(), { first: fn, last: fn }, protocol.adapterCall());
            case 'request':
              return use([fn, fn]);
            case 'reply':
              return makePair();
            case 'validation':
              return check({ first: fn, last: 'bad' as protocol.Flag });
            default:
              return protocol.exportFailureUnchecked(sa.owner(), { first: fn, last: cyclic });
          }
        },
        (error: unknown) =>
          name === 'serialization' ||
          name === 'validation' ||
          (error instanceof DuplexError && error.code === TOO_MANY_EXPORTS),
      );
      assert.deepEqual(sa.counts(), before, `${name}: failed export retained bindings`);
      assert.deepEqual(sb.counts(), remoteBefore, `${name}: reply export retained bindings`);
      assert.equal(used, false, 'failed request reached handler');
      assert.equal(await retained(), 7, 'prior binding invalidated');
      const raw = protocol.exportCall(sa.owner(), fn);
      sa.release(sa.decode(raw));
    }
  }
} finally {
  pa.close();
  pb.close();
}
