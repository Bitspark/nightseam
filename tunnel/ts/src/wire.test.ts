import assert from 'node:assert/strict';
import test from 'node:test';
import { at, mount, pipe, type Endpoint } from '@nightseam/duplex';
import { DuplexPeer, callWire, createDispatcher, handleWire } from '@nightseam/runtime';
import { Tunnel } from './index.ts';

async function tunnels() {
  const [a, b] = pipe();
  const left = new DuplexPeer(),
    right = new DuplexPeer({ role: 'server' });
  await Promise.all([left.attach(a), right.attach(b)]);
  return {
    client: new Tunnel(left),
    server: new Tunnel(right),
    left,
    right,
    close: () => {
      left.close();
      right.close();
    },
  };
}

test('channel is a prepared wire before its first selection or mounted use', async (t) => {
  const pair = await tunnels();
  t.after(pair.close);
  let opens = 0;
  const observer = {
    observe: (event: { type: string }) => {
      if (event.type === 'connection.opened') opens++;
    },
  };
  const digest = 'a'.repeat(64);
  const opened: Endpoint & { id: number; digest: string } = await pair.client.open('wire', digest, { observer });
  const pending = callWire(opened, ['deep', 'echo'], 'ok');
  await Promise.resolve();
  const accepted = await pair.server.accept({
    observer,
    prepare: (peer) => {
      const dispatcher = createDispatcher(peer.wire());
      t.after(() => dispatcher.close());
      handleWire(dispatcher, ['deep', 'echo'], (value) => value);
    },
  });
  assert.equal(await pending, 'ok');
  assert.equal(opened.digest, digest);
  assert.equal(accepted.digest, digest);
  assert.equal(opens, 2);
  const selected = at(mount(new Map([['route', opened]])), ['route', 'deep']);
  assert.equal(await callWire(selected, ['echo'], 'selected'), 'selected');
  assert.equal(await pair.client.channel(opened.id, { prepare: () => assert.fail('lookup rebuilt peer') }), opened);
  assert.equal(opens, 2);
  assert.equal(pair.client.connection(opened.id), undefined);
  accepted.close();
  assert.equal(pair.left.status, 'connected');
  assert.equal(pair.right.status, 'connected');
});

test('raw connection refuses a second wire reader and retains its identity', async (t) => {
  const pair = await tunnels();
  t.after(pair.close);
  const raw = await pair.client.openConnection('raw', '');
  await assert.rejects(pair.client.channel(raw.id), { code: 'channel_invalid' });
  assert.equal(pair.client.connection(raw.id), raw);
});

test('peer preparation runs after validation and cleans registered wire receivers on failure', () => {
  let prepared = 0,
    closed = 0;
  assert.throws(
    () =>
      new DuplexPeer({
        queueCapacity: 0,
        prepare: () => {
          prepared++;
        },
      }),
  );
  assert.equal(prepared, 0);
  assert.throws(
    () =>
      new DuplexPeer({
        prepare: (peer) => {
          prepared++;
          peer.wire().receive({
            closed: () => {
              closed++;
            },
            message: () => {},
          });
          throw new Error('prepare failed');
        },
      }),
    /prepare failed/,
  );
  assert.equal(prepared, 1);
  assert.equal(closed, 1);
  assert.throws(() => new DuplexPeer({ prepare: async () => {} }), {
    code: 'invalid_options',
    message: 'prepare must complete synchronously.',
  });
});

test('server-opened wire channel preserves channel parity and carries calls in both directions', async (t) => {
  const pair = await tunnels();
  t.after(pair.close);
  const options = {
    prepare: (peer: DuplexPeer) => {
      const dispatcher = createDispatcher(peer.wire());
      t.after(() => dispatcher.close());
      handleWire(dispatcher, ['echo'], (value) => value);
    },
  };
  const opened = await pair.server.open('reverse', '', options),
    accepted = await pair.client.accept(options);
  assert.equal(opened.id % 2, 0);
  for (const wire of [opened, accepted]) assert.equal(await callWire(wire, ['echo'], 'both ways'), 'both ways');
});
