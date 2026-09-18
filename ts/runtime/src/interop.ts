/** Invoked by Go runtime integration tests against a real local WebSocket server. */
import assert from 'node:assert/strict';
import { DuplexPeer, DuplexError } from './index.ts';

const peer = new DuplexPeer();
peer.handle('multiply', params => ({ value: (params as { value: number }).value * 3 }));
let noticeResolve!: (value: unknown) => void;
const notice = new Promise<unknown>(resolve => { noticeResolve = resolve; });
peer.onEvent('notice', data => { noticeResolve(data); });

try {
  await peer.connect(process.argv[2]);
  const [echo, reverse] = await Promise.all([
    peer.call('echo', { text: 'TypeScript ↔ Go', values: [null, true, 1] }),
    peer.call('roundtrip', { value: 7 }),
  ]);
  assert.deepEqual(echo, { text: 'TypeScript ↔ Go', values: [null, true, 1] });
  assert.deepEqual(reverse, { value: 21 });
  await assert.rejects(peer.call('fail'), error => error instanceof DuplexError && error.code === 'denied' && error.message === 'Access denied');
  await peer.call('notify');
  assert.deepEqual(await notice, { value: 42 });
  await peer.emit('client_notice', { value: 99 });
  const abort = new AbortController();
  const waiting = peer.call('wait', {}, { signal: abort.signal });
  const cancelled = assert.rejects(waiting, { code: 'cancelled' });
  setTimeout(() => abort.abort(), 25);
  await cancelled;
  // A subsequent round trip proves cancellation didn't break the duplex reader.
  assert.deepEqual(await peer.call('echo', { afterCancel: true }), { afterCancel: true });
  process.stdout.write(JSON.stringify({ ok: true, profile: 'nighthall.duplex/1' }) + '\n');
} finally {
  peer.close();
}
