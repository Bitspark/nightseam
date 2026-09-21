import { strict as assert } from 'node:assert';
import { test } from 'node:test';
import { createInvocation, InvocationError } from './invocation.ts';
import type { InvocationControl, InvocationRouting } from './invocation.ts';
import type { Message } from '@nightseam/duplex';

const cancel: Message = { frame: { version: 1, kind: 'cancel', id: 'c:1' } };
const code = (expected: string) => (error: unknown) => error instanceof InvocationError && error.code === expected;

for (const late of [false, true])
  test(`invocation retains retirement during ${late ? 'late' : 'ready'} capture cancellation`, () => {
    let retired = 0;
    const { owner, routing, execution } = createInvocation({ captures: 3, bodies: 1 }, () => retired++);
    assert.equal('settle' in routing, false);
    assert.equal('begin' in routing, false);
    const body = execution.begin();
    let seen = 0;
    let control: InvocationControl;
    const capture = routing.capture(() => {
      seen++;
      owner.dispatchDone();
      owner.dispatchDone();
      owner.settle();
      body.done();
      body.done();
      control.deliver();
      assert.equal(retired, 0);
    });
    control = owner.queueControl(cancel)!;
    if (late) {
      control.deliver();
      capture.delivered();
    } else {
      capture.delivered();
      control.deliver();
    }
    assert.equal(seen, 1);
    assert.equal(retired, 1);
    control.deliver();
    capture.delivered();
    assert.throws(() => routing.capture(() => {}), code('ended'));
    assert.throws(() => execution.begin(), code('ended'));
  });

test('invocation bounds total captures and bodies and coalesces reentrant controls', () => {
  let retired = 0,
    seen = 0;
  const { owner, routing, execution } = createInvocation({ captures: 2, bodies: 1 }, () => retired++);
  const body = execution.begin();
  assert.throws(() => execution.begin(), code('limit'));
  let ticket: InvocationControl;
  routing
    .capture(() => {
      seen++;
      ticket.deliver();
    })
    .delivered();
  ticket = owner.queueControl(cancel)!;
  ticket.deliver();
  const second = routing.capture((message) => {
    seen++;
    assert.equal(owner.queueControl(message), undefined);
    owner.settle();
    body.done();
  });
  assert.throws(() => routing.capture(() => {}), code('limit'));
  owner.dispatchDone();
  second.delivered();
  assert.equal(seen, 2);
  assert.equal(retired, 1);
});

test('completed invocation handles do not retain admission capacity', () => {
  let retired = 0;
  const retained: InvocationRouting[] = [];
  for (let index = 0; index < 256; index++) {
    const { owner, routing, execution } = createInvocation({ captures: 1, bodies: 1 }, () => retired++);
    routing.capture(() => assert.fail('completed invocation received control')).delivered();
    const body = execution.begin();
    owner.dispatchDone();
    owner.settle();
    assert.equal(retired, retained.length);
    body.done();
    retained.push(routing);
  }
  assert.equal(retired, retained.length);
});
