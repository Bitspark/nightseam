import assert from 'node:assert/strict';
import test from 'node:test';
import type { Endpoint, Message, Receiver } from '@nightseam/duplex';
import { createDispatcher } from './dispatcher.ts';
import { wirePair } from './wire-pair.ts';
import { callWire, handleWire } from './wire.ts';

test('one dispatcher attachment supports siblings and leaves its borrowed endpoint usable', async () => {
  const [left, right] = wirePair();
  try {
    const dispatch = createDispatcher(right);
    assert.throws(() => right.receive({}), { code: 'receiver_exists' });
    for (const name of ['a', 'b']) {
      const binding = createDispatcher(dispatch.select([name]));
      handleWire(binding, ['read'], () => name);
    }
    for (const name of ['a', 'b']) assert.equal(await callWire(left, [name, 'read']), name);
    dispatch.close();
    const rebound = createDispatcher(right);
    handleWire(rebound, ['read'], () => 'rebound');
    assert.equal(await callWire(left, ['read']), 'rebound');
  } finally {
    left.close();
  }
});

test('an unmanaged invocation is explicitly refused with its original return capability', () => {
  let attached: Receiver | undefined;
  const endpoint: Endpoint = {
    send() {},
    receive(receiver) {
      attached = receiver;
      return () => {};
    },
    close() {},
  };
  const dispatch = createDispatcher(endpoint);
  let called = false;
  dispatch.register([], {
    message() {
      called = true;
    },
  });
  let reply: Message | undefined;
  // A return capability refuses what it does not implement, as every addressed
  // receiver in this profile does; this one carries outcomes and nothing else.
  const address = {
    wire: {
      send(path: readonly string[], message: Message) {
        if (path.length) throw new Error('this return capability carries outcomes only');
        reply = message;
      },
    },
  };
  attached!.message!([], { frame: { version: 1, kind: 'request', id: 'one', params: null }, return: address });
  assert.equal(called, false);
  assert.equal(reply?.frame.kind, 'response');
  if (reply?.frame.kind === 'response') assert.equal(reply.frame.error?.code, 'invalid_message');
});

test('an explicitly owned dispatcher closes its endpoint', () => {
  const [left, right] = wirePair();
  try {
    const dispatcher = createDispatcher(right, { ownEndpoint: true });
    dispatcher.close(1002, 'wire event rejected');
    assert.throws(() => right.receive({}), { code: 'disconnected' });
    dispatcher.close();
  } finally {
    left.close();
  }
});
