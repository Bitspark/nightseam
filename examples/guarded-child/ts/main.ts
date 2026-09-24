import assert from 'node:assert/strict';
import { at, Declared } from '@nightseam/duplex';
import { callWire, createDispatcher, handleWire, wirePair, UnpublishedError } from '@nightseam/runtime';
import type { Wire } from '@bitspark/bitwire';

// State and lifecycle belong to the application. Composition borrows access.
const items = ['book', 'pen'];
const [caller, callee] = wirePair();
const dispatcher = createDispatcher(callee);
handleWire(dispatcher, ['storeInfo'], () => 'Bookshop');
handleWire(dispatcher, ['cart', 'list'], () => [...items]);
handleWire(dispatcher, ['cart', 'add'], (item) => {
  assert.equal(typeof item, 'string');
  items.push(item as string);
  return null;
});
handleWire(dispatcher, ['recommendations'], () => ['pencil']);
const origin = at(caller, ['storeInfo']);
const cart = at(caller, ['cart']);
const recommendations = at(caller, ['recommendations']);
const options = { timeoutMs: 10_000 };

try {
  let remaining = 2;
  const budgetExceeded = new Error('cart admission budget exhausted');
  // A consumer policy: charge new add requests, pass lists and controls through.
  const guarded: Wire = {
    send(path, message) {
      const charged = message.frame.kind === 'request' && path.length === 1 && path[0] === 'add';
      if (charged && remaining === 0) throw budgetExceeded;
      cart.send(path, message);
      if (charged) remaining--;
    },
  };
  const shop = Declared.compose(origin, [['cart', guarded]]);
  await callWire(shop.bind(), ['cart', 'add'], 'notebook', options);
  assert.equal(remaining, 1);
  console.log('remaining after first add:', remaining);
  const parts = shop.decompose();
  assert.equal(parts.children[0]![1], guarded);
  const expanded = Declared.compose(parts.origin, [...parts.children, ['recommendations', recommendations]]);
  await callWire(expanded.bind(), ['cart', 'add'], 'pencil', options);
  assert.equal(remaining, 0);
  console.log('remaining after rebuild and second add:', remaining);
  await assert.rejects(
    callWire(expanded.bind(), ['cart', 'add'], 'eraser', options),
    (error: unknown) => error instanceof UnpublishedError && error.cause === budgetExceeded,
  );
  console.log('third add: refused');
  const contents = await callWire<string[]>(shop.bind(), ['cart', 'list'], null, options);
  assert.deepEqual(contents, ['book', 'pen', 'notebook', 'pencil']);
  console.log('cart:', contents.join(', '));
} finally {
  dispatcher.close();
  caller.close();
  callee.close();
}
