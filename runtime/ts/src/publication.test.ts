import assert from 'node:assert/strict';
import test from 'node:test';
import { pipe, type FrameConnection } from '@nightseam/duplex';
import { DuplexError, DuplexPeer, UnpublishedError } from './index.ts';

function deferred<V>(): { promise: Promise<V>; resolve: (value: V) => void } {
  let resolve!: (value: V) => void;
  const promise = new Promise<V>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

test('unpublished proof belongs to the local send attempt', async () => {
  const [a, b] = pipe();
  const client = new DuplexPeer({ maxPendingRequests: 1, maxFrameBytes: 512 });
  const server = new DuplexPeer({ role: 'server' });
  const entered = deferred<void>();
  const finish = deferred<void>();
  server.handle('wait', async () => {
    entered.resolve();
    await finish.promise;
    return null;
  });
  server.handle('busy', async () => {
    throw new DuplexError('busy', 'retained before refusing');
  });
  server.handle('nested', async () => {
    const controller = new AbortController();
    controller.abort();
    return server.call('never.sent', undefined, { signal: controller.signal });
  });
  await Promise.all([client.attach(a), server.attach(b)]);
  try {
    const controller = new AbortController();
    controller.abort();
    await assert.rejects(client.call('wait', undefined, { signal: controller.signal }), (error: unknown) => {
      assert.ok(error instanceof UnpublishedError);
      assert.ok(error instanceof DuplexError);
      assert.equal(error.code, 'cancelled');
      assert.ok(error.cause instanceof DuplexError);
      assert.equal(error.cause.code, 'cancelled');
      return true;
    });
    for (const request of [() => 1, 'x'.repeat(1024)]) {
      await assert.rejects(client.call('wait', request), UnpublishedError);
      await assert.rejects(client.emit('event', request), UnpublishedError);
    }
    const held = client.call('wait');
    await entered.promise;
    await assert.rejects(client.call('busy'), (error: unknown) => {
      assert.ok(error instanceof UnpublishedError);
      assert.equal(error.code, 'busy');
      return true;
    });
    finish.resolve();
    await held;
    for (const [method, code] of [
      ['busy', 'busy'],
      ['nested', 'cancelled'],
    ]) {
      await assert.rejects(client.call(method!), (error: unknown) => {
        assert.ok(error instanceof DuplexError);
        assert.equal(error.code, code);
        assert.ok(!(error instanceof UnpublishedError), 'remote error carried local publication proof');
        return true;
      });
    }
  } finally {
    finish.resolve();
    client.close();
    server.close();
  }
});

test('a queued write failure has no unpublished proof', async () => {
  const [a, b] = pipe();
  const cause = new DuplexError('send_failed', 'transport failed after queue acceptance');
  const attempted = deferred<void>();
  const connection: FrameConnection = {
    get state() {
      return a.state;
    },
    get buffered() {
      return a.buffered;
    },
    send() {
      attempted.resolve();
      throw cause;
    },
    close: (code, reason) => a.close(code, reason),
    listen: (handlers) => a.listen(handlers),
  };
  const peer = new DuplexPeer();
  await peer.attach(connection);
  try {
    await assert.rejects(peer.call('supply'), (error: unknown) => {
      assert.ok(error instanceof DuplexError);
      assert.ok(!(error instanceof UnpublishedError));
      assert.equal(error.code, cause.code);
      return true;
    });
    await attempted.promise;
  } finally {
    peer.close();
    b.close();
  }
});

test('a refused reverse reply cannot lend its proof to an already delivered call', async () => {
  const [a, b] = pipe();
  const client = new DuplexPeer({ maxFrameBytes: 160 });
  const server = new DuplexPeer({ role: 'server' });
  let delivered = false;
  client.handle('b', async () => 'x'.repeat(2000));
  server.handle('a', async () => {
    delivered = true;
    return server.call('b');
  });
  await Promise.all([client.attach(a), server.attach(b)]);
  try {
    await assert.rejects(client.call('a'), (error: unknown) => {
      assert.equal(delivered, true);
      assert.ok(error instanceof DuplexError);
      assert.equal(error.code, 'frame_too_large');
      assert.ok(!(error instanceof UnpublishedError), 'another reply lent proof to this delivered request');
      return true;
    });
  } finally {
    client.close();
    server.close();
  }
});
