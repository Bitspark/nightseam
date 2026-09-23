import assert from 'node:assert/strict';
import { test } from 'node:test';
import type { Message, Path, Wire } from '@bitspark/bitwire';
import {
  at,
  Declared,
  DeclaredError,
  permitAdmission,
  refusingOrigin,
  WireError,
  type AdmissionPolicy,
} from './index.ts';

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
function quota(remaining: number) {
  const paths: Path[] = [];
  const policy: AdmissionPolicy = {
    admit(path) {
      paths.push([...path]);
      if (remaining === 0) throw new Error('quota');
      if (remaining > 0) remaining--;
    },
  };
  return { paths, policy };
}

test('own origin survives exact fresh-child attachment; missing children never fall back', () => {
  const parent = origin(),
    child = origin();
  const root = Declared.compose({ own: parent.wire, policy: permitAdmission }, []);
  const leaf = Declared.compose({ own: child.wire, policy: permitAdmission }, []);
  const grown = root.attach([], '', leaf);
  const message: Message = { ...event(), return: { wire: parent.wire } };
  grown.bind().send([], message);
  grown.bind().send([''], message);
  assert.deepEqual(
    parent.deliveries.map((d) => d.path),
    [[]],
  );
  assert.deepEqual(
    child.deliveries.map((d) => d.path),
    [[]],
  );
  assert.equal(parent.deliveries[0]!.message, message);
  assert.equal(child.deliveries[0]!.message.return, message.return);
  assert.equal(grown.decompose().value.own, parent.wire);
  assert.equal(root.at(['']), undefined);
  assert.throws(() => grown.bind().send(['missing'], message), { code: 'no_route' });
  assert.equal(parent.deliveries.length, 1);
  assert.throws(() => grown.attach([], '', leaf), { code: 'child_exists' });
  assert.throws(() => root.attach(['missing'], 'x', leaf), { code: 'no_route' });
});

test('parts retain raw child aliases and shared policy state; selected access retains guards once', () => {
  const target = origin(),
    gate = quota(2);
  const leaf = Declared.compose({ own: target.wire, policy: permitAdmission }, []);
  const input: [string, Declared][] = [
    ['x', leaf],
    ['y', leaf],
  ];
  const root = Declared.compose({ own: refusingOrigin, policy: gate.policy }, input);
  input.length = 0;
  const parts = root.decompose();
  const rebuilt = Declared.compose(parts.value, parts.children);
  assert.equal(parts.value.policy, gate.policy);
  assert.equal(parts.children[0]![1], leaf);
  assert.equal(parts.children[1]![1], leaf);
  parts.children.length = 0;
  at(root.bind(), ['x']).send([], event());
  at(rebuilt.bind(), ['y']).send([], event());
  assert.throws(() => rebuilt.bind().send(['x'], event()), /quota/);
  assert.deepEqual(gate.paths, [['x'], ['y'], ['x']]);
  assert.equal(target.deliveries.length, 2);
  assert.deepEqual(Object.keys(root.bind()), ['send']);
});

test('one policy is checked at each occurrence, before missing-child refusal; controls bypass new admission', () => {
  const target = origin(),
    gate = quota(-1);
  const leaf = Declared.compose({ own: target.wire, policy: gate.policy }, []);
  const root = Declared.compose({ own: refusingOrigin, policy: gate.policy }, [['x', leaf]]);
  root.bind().send(['x'], event());
  assert.deepEqual(gate.paths, [['x'], []]);
  assert.throws(() => root.bind().send(['absent'], event()), WireError);
  for (const message of [
    { frame: { version: 1, kind: 'response', id: 'c:1', result: null } },
    { frame: { version: 1, kind: 'cancel', id: 'c:1' } },
  ] satisfies Message[]) {
    assert.throws(() => root.bind().send(['x'], message), { code: 'invalid_frame' });
  }
  assert.equal(gate.paths.length, 3);
});

test('construction refuses duplicate keys and missing parts; paths retain opaque scalar segments', () => {
  const value = { own: refusingOrigin, policy: permitAdmission };
  const leaf = Declared.compose(value, []);
  assert.throws(
    () =>
      Declared.compose(value, [
        ['x', leaf],
        ['x', leaf],
      ]),
    { code: 'child_exists' },
  );
  assert.throws(() => Declared.compose(value, [['\ud800', leaf]]), { code: 'invalid_path' });
  assert.throws(() => Declared.compose({ ...value, own: undefined as unknown as Wire }, []), DeclaredError);
  assert.throws(
    () => Declared.compose({ ...value, policy: undefined as unknown as AdmissionPolicy }, []),
    DeclaredError,
  );
  const root = Declared.compose(value, [
    ['a/b', leaf],
    ['é', leaf],
    ['e\u0301', leaf],
  ]);
  for (const key of ['a/b', 'é', 'e\u0301']) assert.equal(root.at([key]), leaf);
  assert.equal(root.at(['a', 'b']), undefined);
  assert.throws(() => root.bind().send(['\ud800'], event()), { code: 'invalid_path' });
});

test('attachment retains ancestor values and the previous bound view', () => {
  const target = origin(),
    gate = quota(3);
  const leaf = Declared.compose({ own: target.wire, policy: permitAdmission }, []);
  const root = Declared.compose({ own: refusingOrigin, policy: gate.policy }, [['a', leaf]]);
  const old = at(root.bind(), ['a']);
  const next = root.attach(['a'], 'b', leaf);
  old.send([], event());
  assert.throws(() => old.send(['b'], event()), { code: 'no_route' });
  next.bind().send(['a', 'b'], event());
  assert.equal(target.deliveries.length, 2);
  assert.equal(next.at(['a'])!.decompose().value.own, leaf.decompose().value.own);
  assert.equal(next.decompose().value.policy, gate.policy);
});

const request = (id: string): Message => ({
  frame: { version: 1, kind: 'request', id, params: null },
  return: { wire: refusingOrigin },
});
const cancelOf = (sent: Message): Message => ({
  frame: { version: 1, kind: 'cancel', id: (sent.frame as { id: string }).id },
  ...(sent.return ? { return: sent.return } : {}),
});

test('a cancel reaches the destination its request was admitted to, once and unchecked, across rebuild and rebind', () => {
  const first = origin(),
    second = origin(),
    gate = quota(-1);
  const branch = Declared.compose({ own: refusingOrigin, policy: permitAdmission }, [
    ['b', Declared.compose({ own: first.wire, policy: permitAdmission }, [])],
  ]);
  const root = Declared.compose({ own: refusingOrigin, policy: gate.policy }, [['a', branch]]);
  const bound = root.bind();
  const selected = at(bound, ['a']);
  const direct = request('c:1'),
    viewed = request('c:1');
  bound.send(['a', 'b'], direct);
  selected.send(['b'], viewed);
  const checks = gate.paths.length;

  // Rebuild every node from its parts, then rebind a/b to another destination.
  const { value } = root.decompose();
  const rebound = Declared.compose(value, [
    [
      'a',
      Declared.compose(branch.decompose().value, [
        ['b', Declared.compose({ own: second.wire, policy: permitAdmission }, [])],
      ]),
    ],
  ]);
  assert.throws(() => rebound.bind().send(['a', 'b'], cancelOf(direct)), { code: 'invalid_frame' });
  bound.send(['a', 'b'], cancelOf(direct));
  selected.send(['b'], cancelOf(viewed));
  assert.equal(gate.paths.length, checks, 'a cancel entered admission again');
  assert.equal(second.deliveries.length, 0);
  assert.deepEqual(
    first.deliveries.map(({ path, message }) => [path, message.frame.kind, message.return]),
    [
      [[], 'request', direct.return],
      [[], 'request', viewed.return],
      [[], 'cancel', direct.return],
      [[], 'cancel', viewed.return],
    ],
  );
  assert.throws(() => bound.send(['a', 'b'], cancelOf(direct)), { code: 'invalid_frame' });
});

test('refused requests leave nothing to cancel', () => {
  const target = origin();
  const root = Declared.compose({ own: refusingOrigin, policy: permitAdmission }, [
    ['guarded', Declared.compose({ own: target.wire, policy: quota(0).policy }, [])],
    ['gone', Declared.compose({ own: refusingOrigin, policy: permitAdmission }, [])],
  ]);
  const bound = root.bind();
  for (const path of [['guarded'], ['gone'], ['missing']]) {
    const sent = request('c:1');
    assert.throws(() => bound.send(path, sent));
    assert.throws(() => bound.send(path, cancelOf(sent)), { code: 'invalid_frame' });
  }
  assert.equal(target.deliveries.length, 0);
});
