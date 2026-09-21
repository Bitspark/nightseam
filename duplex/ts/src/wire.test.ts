import assert from 'node:assert/strict';
import test from 'node:test';
import { at, mount, encodePath, decodePath, WireError } from './wire.ts';
import type { Wire, Message, Receiver, Path, ProfileFrame } from './wire.ts';

test('path encoding is canonical, injective and composes by concatenation', () => {
  const paths: Path[] = [[], [''], ['a', 'b'], ['a.b'], ['a', 'b:c'], ['é', 'e\u0301', '😀', '\ufeff', '\0']];
  const seen = new Set<string>();
  for (const path of paths) {
    const encoded = encodePath(path);
    assert.equal(seen.has(encoded), false);
    seen.add(encoded);
    assert.deepEqual(decodePath(encoded), path);
    for (const suffix of paths) assert.equal(encodePath([...path, ...suffix]), encoded + encodePath(suffix));
  }
  assert.equal(encodePath(['a', '😀', '']), '1:a4:😀0:');
  for (const malformed of [
    '01:a',
    '00:',
    '1',
    ':',
    '-1:a',
    '2:a',
    '1:é',
    '99999999999999999999999999999:x',
    '1:\ud800',
  ])
    assert.throws(() => decodePath(malformed));
  assert.throws(() => encodePath(['\ud800']));
  assert.throws(() => encodePath(['\udc00']));
});

// A controlled dispatch fixture: no scheduler timing or physical-carrier claim.
class QueuedRoot implements Wire {
  readonly queue: { path: Path; message: Message }[] = [];
  readonly receivers = new Map<string, Receiver>();
  closes = 0;
  send(path: Path, message: Message): void {
    if (this.closes) throw new WireError('closed');
    encodePath(path);
    this.queue.push({ path: [...path], message });
  }
  receive(path: Path, receiver: Receiver): () => void {
    if (this.closes) throw new WireError('closed');
    const key = encodePath(path);
    if (this.receivers.has(key)) throw new WireError('receiver_exists');
    this.receivers.set(key, receiver);
    let active = true;
    return () => {
      if (active) {
        active = false;
        this.receivers.delete(key);
      }
    };
  }
  close(code = 1000, reason = ''): void {
    if (this.closes) return;
    this.closes++;
    const receivers = [...this.receivers.values()];
    this.receivers.clear();
    for (const receiver of receivers) receiver.closed?.(code, reason);
  }
  drain(): void {
    for (let entry = this.queue.shift(); entry; entry = this.queue.shift())
      this.receivers.get(encodePath(entry.path))?.message?.([], entry.message);
  }
}

test('selection and mount delegate to the existing queued root and preserve every frame and return capability', () => {
  const root = new QueuedRoot(),
    reply = new QueuedRoot();
  const address = { wire: reply };
  const prefix = ['a.b'];
  const selected = at(root, prefix);
  prefix[0] = 'changed';
  const children = new Map<string, Wire>([['x', selected]]);
  const mounted = mount(children);
  children.set('x', reply);
  const view = at(at(mounted, ['x']), ['😀']);
  const received: Message[] = [];
  const detach = view.receive(['call'], {
    message: (path, message) => {
      assert.deepEqual(path, []);
      received.push(message);
    },
  });
  const frames: ProfileFrame[] = [
    { version: 1, kind: 'request', id: 'c:1', params: { n: 42 }, meta: { tag: 'value' } },
    { version: 1, kind: 'response', id: 'c:1', error: { code: 'refused', message: 'No', data: { why: 'test' } } },
    { version: 1, kind: 'event', data: null },
    { version: 1, kind: 'cancel', id: 'c:1' },
  ];
  for (const frame of frames) view.send(['call'], { frame, return: address });
  assert.equal(received.length, 0, 'destination ran inside send');
  assert.equal(root.queue.length, 4);
  assert.equal(reply.queue.length, 0);
  for (const entry of root.queue) assert.deepEqual(entry.path, ['a.b', '😀', 'call']);
  root.drain();
  assert.equal(received.length, 4);
  for (let i = 0; i < frames.length; i++) {
    assert.equal(received[i]!.frame, frames[i]);
    assert.equal(received[i]!.return, address);
  }
  assert.throws(() => at(root, ['a.b', '😀']).receive(['call'], {}), { code: 'receiver_exists' });
  detach();
  detach();
  at(root, []).receive(['a.b', '😀', 'call'], {})();
});

test('mount close detaches its registrations and preserves its children', () => {
  const root = new QueuedRoot(),
    mounted = mount(new Map([['', root]])),
    selected = at(mounted, ['']);
  let closed = 0;
  selected.receive(['call'], {
    closed: (code, reason) => {
      closed++;
      assert.equal(code, 1000);
      assert.equal(reason, 'mount ended');
      mounted.close(code, reason);
    },
  });
  assert.throws(() => mounted.receive([], {}), { code: 'no_route' });
  mounted.close(1000, 'mount ended');
  mounted.close(1000, 'again');
  assert.equal(closed, 1);
  assert.equal(root.closes, 0);
  assert.equal(root.receivers.size, 0);
  const message: Message = { frame: { version: 1, kind: 'cancel', id: 'c:1' } };
  assert.throws(() => selected.send(['call'], message), { code: 'closed' });
  assert.throws(() => selected.receive(['call'], {}), { code: 'closed' });
  root.send(['call'], message);
  root.receive(['call'], {});
  at(root, ['call']).close();
  assert.equal(root.closes, 1);
});

test('mount close during receive cannot leave a child registration', () => {
  const root = new QueuedRoot();
  let mounted: Wire;
  const child: Wire = {
    send: (path, message) => root.send(path, message),
    receive: (path, receiver) => {
      const detach = root.receive(path, receiver);
      mounted.close(1000, 'done');
      return detach;
    },
    close: (code, reason) => root.close(code, reason),
  };
  mounted = mount(new Map([['x', child]]));
  let closed = 0;
  assert.throws(
    () =>
      mounted.receive(['x', 'call'], {
        closed: () => {
          closed++;
        },
      }),
    { code: 'closed' },
  );
  assert.equal(root.receivers.size, 0);
  assert.equal(root.closes, 0);
  assert.equal(closed, 1);
});

test('a mounted child closing does not end its sibling', () => {
  const left = new QueuedRoot(),
    right = new QueuedRoot();
  const mounted = mount(
    new Map([
      ['left', left],
      ['right', right],
    ]),
  );
  let closed = 0;
  mounted.receive(['left', 'call'], {
    closed: () => {
      closed++;
    },
  });
  left.close(1000, 'child ended');
  mounted.send(['right', 'call'], { frame: { version: 1, kind: 'event', data: null } });
  mounted.close(1000, 'mount ended');
  assert.equal(closed, 1);
  assert.equal(right.closes, 0);
  assert.equal(right.queue.length, 1);
});
