/** Invoked by the Go tunnel's interoperability test against a real local WebSocket server. */
import assert from 'node:assert/strict';
import { DuplexPeer } from '@nightseam/runtime';
import { Tunnel } from './index.ts';

const outer = new DuplexPeer();
const done = new Promise<unknown>(resolve => { outer.onEvent('done', resolve); });
try {
  await outer.connect(process.argv[2]);
  const tunnel = new Tunnel(outer);
  // This side opens a channel and speaks the profile over it to Go.
  const outbound = await tunnel.open('probe', 3);
  const client = new DuplexPeer({ role: 'client' });
  await client.attach(outbound);
  assert.deepEqual(await client.call('echo', { via: 'tunnel', binary: false }), { via: 'tunnel', binary: false });
  // Go opens a channel to this side, which serves it.
  const inbound = await tunnel.accept();
  assert.equal(inbound.family, 'codex');
  assert.equal(inbound.after, 11);
  const server = new DuplexPeer({ role: 'server' });
  server.handle('multiply', params => ({ value: (params as { value: number }).value * 3 }));
  await server.attach(inbound);
  assert.deepEqual(await done, { ok: true });
  client.close();
  server.close();
  process.stdout.write(JSON.stringify({ ok: true, opened: outbound.id, accepted: inbound.id }) + '\n');
} finally {
  outer.close();
}
