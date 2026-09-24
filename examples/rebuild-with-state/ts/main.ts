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
  const shop = Declared.compose(origin, [['cart', cart]]);
  const before = await callWire<string[]>(shop.bind(), ['cart', 'list'], null, options);
  assert.deepEqual(before, ['book', 'pen']);
  // Copy the construction containers and retain the complete cart capability.
  const parts = shop.decompose();
  const sameCart = parts.children.length === 1 && parts.children[0]![1] === cart;
  assert.ok(sameCart);
  assert.equal(parts.origin, origin);
  const expanded = Declared.compose(parts.origin, [...parts.children, ['recommendations', recommendations]]);
  await callWire(expanded.bind(), ['cart', 'add'], 'notebook', options);
  // Old and new access both reach the same cart.
  const after = await callWire<string[]>(shop.bind(), ['cart', 'list'], null, options);
  assert.deepEqual(after, ['book', 'pen', 'notebook']);
  assert.deepEqual(await callWire(expanded.bind(), ['cart', 'list'], null, options), after);
  const name = await callWire<string>(expanded.bind(), [], null, options);
  const suggestions = await callWire<string[]>(expanded.bind(), ['recommendations'], null, options);
  assert.equal(name, 'Bookshop');
  assert.deepEqual(suggestions, ['pencil']);
  console.log('before:', before.join(', '));
  console.log('same cart capability:', sameCart);
  console.log('shop:', name);
  console.log('after:', after.join(', '));
  console.log('recommendations:', suggestions.join(', '));
} finally {
  dispatcher.close();
  caller.close();
  callee.close();
}
