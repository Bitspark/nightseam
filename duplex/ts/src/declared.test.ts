import assert from 'node:assert/strict';
import { test } from 'node:test';
import type { Endpoint, Message, Path, Wire } from '@bitspark/bitwire';
import { at, Declared, refusingOrigin, type DeclaredChild } from './index.ts';

const event = (): Message => ({ frame: { version: 1, kind: 'event', data: null } });
function origin() {
  const deliveries: { path: Path; message: Message }[] = [];
  const wire: Wire = {
    send(path, message) {
      deliveries.push({ path, message });
    },
  };
  return { deliveries, wire };
}

test('every frame is delegated unchanged to the origin or a complete opaque child', () => {
  const own = origin(),
    child = origin();
  const root = Declared.compose(own.wire, [['a', child.wire]]);
  const capability = { wire: own.wire };
  const messages: Message[] = [
    { frame: { version: 1, kind: 'request', id: 'same', params: { kept: true } }, return: capability },
    { frame: { version: 1, kind: 'event', data: { kept: true } }, return: capability },
    { frame: { version: 1, kind: 'response', id: 'same', result: { kept: true } }, return: capability },
    { frame: { version: 1, kind: 'cancel', id: 'same' }, return: capability },
  ];
  const access = root.bind();
  for (const message of messages) {
    access.send([], message);
    access.send(['a', 'opaque', 'suffix'], message);
    assert.equal(own.deliveries.at(-1)!.message, message);
    assert.equal(child.deliveries.at(-1)!.message, message);
  }
  assert.deepEqual(
    own.deliveries.map((d) => d.path),
    [[], [], [], []],
  );
  assert.deepEqual(
    child.deliveries.map((d) => d.path),
    Array.from({ length: 4 }, () => ['opaque', 'suffix']),
  );
  assert.throws(() => access.send(['missing'], event()), { code: 'no_route' });
  assert.equal(own.deliveries.length, 4);
  assert.equal(child.deliveries.length, 4);
});

test('opaque guard aliases keep their state and refusal through reconstruction', () => {
  const target = origin();
  let remaining = 2,
    checks = 0;
  const refused = new Error('guard exhausted');
  const guard: Wire = {
    send(path, message) {
      checks++;
      if (remaining === 0) throw refused;
      remaining--;
      target.wire.send(path, message);
    },
  };
  const root = Declared.compose(refusingOrigin, [
    ['a', guard],
    ['alias', guard],
  ]);
  const { origin: own, children } = root.decompose();
  assert.equal(children[0]![1], guard);
  assert.equal(children[1]![1], guard);
  const rebuilt = Declared.compose(own, children);
  at(root.bind(), ['a']).send(['tail'], event());
  at(rebuilt.bind(), ['alias']).send(['tail'], event());
  assert.throws(
    () => rebuilt.bind().send(['a'], event()),
    (error) => error === refused,
  );
  assert.equal(checks, 3);
  assert.equal(target.deliveries.length, 2);
});

test('construction and decomposition copy containers and preserve exact UTF-8 keys', () => {
  const own = origin(),
    child = origin();
  const keys = ['\u{10000}', '\ue000', 'é', 'e\u0301', 'a/b', '', '\ufeff'];
  const entries: [string, Wire][] = keys.map((key) => [key, child.wire]);
  const root = Declared.compose(own.wire, entries);
  entries[0]![0] = 'replaced';
  entries[0]![1] = own.wire;
  entries.length = 0;
  const parts = root.decompose();
  assert.equal(parts.origin, own.wire);
  assert.deepEqual(
    parts.children.map(([key]) => key),
    ['', 'a/b', 'e\u0301', 'é', '\ue000', '\ufeff', '\u{10000}'],
  );
  for (const [key, access] of parts.children) {
    assert.equal(access, child.wire);
    root.bind().send([key, 'tail'], event());
  }
  (parts.children[0] as [string, Wire])[0] = 'mutated';
  parts.children.length = 0;
  assert.deepEqual(root.decompose().children[0], ['', child.wire]);
  assert.deepEqual(
    child.deliveries.map((d) => d.path),
    keys.map(() => ['tail']),
  );
  assert.throws(() => root.bind().send(['a', 'b'], event()), { code: 'no_route' });
});

test('nested selected access stays captured when the assembler rebuilds and rebinds', () => {
  const old = origin(),
    replacement = origin();
  const branch = Declared.compose(refusingOrigin, [['run', at(old.wire, ['original'])]]);
  let root = Declared.compose(refusingOrigin, [['svc', branch.bind()]]);
  const captured = at(at(root.bind(), ['svc']), ['run']);
  const { origin: own, children } = root.decompose();
  const rebuilt = Declared.compose(own, children);
  root = Declared.compose(replacement.wire, []);
  captured.send(['tail'], event());
  at(rebuilt.bind(), ['svc', 'run']).send(['tail'], event());
  root.bind().send([], event());
  assert.deepEqual(
    old.deliveries.map((d) => d.path),
    [
      ['original', 'tail'],
      ['original', 'tail'],
    ],
  );
  assert.equal(replacement.deliveries.length, 1);
});

test('construction validates missing capabilities, duplicate and scalar keys; delegation copies paths', () => {
  const valid = origin().wire;
  for (const missing of [undefined, null, {}, { send: 4 }]) {
    assert.throws(() => Declared.compose(missing as unknown as Wire, []), { code: 'invalid_value' });
    assert.throws(() => Declared.compose(valid, [['hole', missing as unknown as Wire]]), { code: 'invalid_value' });
  }
  assert.throws(
    () =>
      Declared.compose(valid, [
        ['x', valid],
        ['x', valid],
      ]),
    { code: 'child_exists' },
  );
  for (const key of ['\ud800', '\udfff', 3]) {
    assert.throws(() => Declared.compose(valid, [[key, valid] as DeclaredChild]), { code: 'invalid_path' });
  }
  const mutator: Wire = {
    send(path) {
      (path as string[])[0] = 'changed';
    },
  };
  const root = Declared.compose(valid, [['x', mutator]]);
  const path = ['x', 'tail'];
  root.bind().send(path, event());
  assert.deepEqual(path, ['x', 'tail']);
  assert.throws(() => root.bind().send(['x', '\ud800'], event()), { code: 'invalid_path' });
});

test('the facade grants no parts or lifecycle and composition acquires no endpoint ownership', () => {
  const target = origin();
  let received = false,
    closed = false;
  const endpoint: Endpoint = {
    ...target.wire,
    receive() {
      received = true;
      return () => {};
    },
    close() {
      closed = true;
    },
  };
  const root = Declared.compose(endpoint, [['child', endpoint]]);
  const access = root.bind();
  assert.deepEqual(Object.keys(access), ['send']);
  assert.equal('decompose' in access, false);
  assert.equal('receive' in access, false);
  assert.equal('close' in access, false);
  assert.equal('send' in root, false);
  const { origin: own, children } = root.decompose();
  const rebuilt = Declared.compose(own, children);
  for (const wire of [access, rebuilt.bind(), endpoint]) wire.send([], event());
  assert.equal(received, false);
  assert.equal(closed, false);
});
