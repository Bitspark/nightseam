import assert from 'node:assert/strict';
import { at, Declared, refusingOrigin } from '@nightseam/duplex';
import { callWire, createDispatcher, handleWire, wirePair } from '@nightseam/runtime';

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
  const shop = Declared.compose(origin, [['cart', cart]]).bind();
  const selected = at(shop, ['cart']);
  const checkout = Declared.compose(refusingOrigin, [['basket', selected]]).bind();
  await callWire(checkout, ['basket', 'add'], 'notebook', options);
  const original = await callWire<string[]>(shop, ['cart', 'list'], null, options);
  const remounted = await callWire<string[]>(checkout, ['basket', 'list'], null, options);
  assert.deepEqual(original, ['book', 'pen', 'notebook']);
  assert.deepEqual(remounted, original);
  console.log('original cart:', original.join(', '));
  console.log('remounted cart:', remounted.join(', '));
} finally {
  dispatcher.close();
  caller.close();
  callee.close();
}
