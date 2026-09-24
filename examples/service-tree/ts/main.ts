import assert from 'node:assert/strict';
import { at, Declared } from '@nightseam/duplex';
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
  const name = await callWire<string>(shop, [], null, options);
  const contents = await callWire<string[]>(shop, ['cart', 'list'], null, options);
  assert.equal(name, 'Bookshop');
  assert.deepEqual(contents, ['book', 'pen']);
  console.log('shop:', name);
  console.log('cart:', contents.join(', '));
} finally {
  dispatcher.close();
  caller.close();
  callee.close();
}
