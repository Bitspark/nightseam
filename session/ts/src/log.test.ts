import assert from 'node:assert/strict';
import test from 'node:test';
import { pipe } from '@nightseam/duplex';
import { memoryLog, Registry, type Frame, type Log } from './index.ts';

const governance = { decides: () => false, asks: () => false };
const frame = (): Frame => ({
  sequence: 0,
  direction: 'down',
  origin: '',
  at: new Date(),
  message: { version: 1, kind: 'event', event: 'changed', data: {} },
  truncated: false,
});

for (const withHead of [true, false]) {
  test(`bind ${withHead ? 'uses the head without replay' : 'replays a log without a head'}`, async () => {
    const beneath = memoryLog(1024);
    for (let i = 0; i < 3; i++) await beneath.append(frame());
    let heads = 0;
    let replays = 0;
    const log: Log = {
      append: (f) => beneath.append(f),
      replay: (after, deliver) => {
        replays++;
        return beneath.replay(after, deliver);
      },
      ...(withHead
        ? {
            head: async () => {
              heads++;
              return 3;
            },
          }
        : {}),
    };
    const registry = new Registry();
    const [machine, up] = pipe();
    const recorded = new Promise<number>((resolve) =>
      registry.onChange((change) => {
        if (change.kind === 'frame_appended') resolve(change.sequence!);
      }),
    );
    registry.bind('s', up, governance, log);
    machine.send({ kind: 'text', data: JSON.stringify(frame().message) });
    assert.equal(await recorded, 4);
    assert.equal(heads, withHead ? 1 : 0);
    assert.equal(replays, withHead ? 0 : 1);
    const [consumer, down] = pipe();
    const replayed = new Promise<number>((resolve) =>
      consumer.listen({
        frame: (f) => {
          if (f.kind !== 'text') return;
          const message = JSON.parse(f.data);
          if (message.event === 'session.cursor' && message.data.sequence === 4) resolve(4);
        },
        close: () => {},
      }),
    );
    registry.attach('s', down, 'observer', 'one', 0);
    assert.equal(await replayed, 4);
    assert.equal(replays, withHead ? 1 : 2);
    machine.close();
  });
}

test('memory log reports its head including zero', async () => {
  const log = memoryLog(1024);
  assert.equal(typeof log.head, 'function');
  for (let i = 0; i < 4; i++) {
    assert.equal(await log.head!(), i);
    await log.append(frame());
  }
});

for (const failure of ['head', 'replay', 'negative', 'fractional', 'unsafe'] as const) {
  test(`a failed initial ${failure} lookup ends the session without processing queued frames`, async () => {
    let replays = 0;
    let appends = 0;
    const log: Log = {
      append: async () => {
        appends++;
        return 1;
      },
      replay: async () => {
        replays++;
        throw new Error('unavailable');
      },
      ...(failure === 'replay'
        ? {}
        : {
            head: async () => {
              if (failure === 'negative') return -1;
              if (failure === 'fractional') return 1.5;
              if (failure === 'unsafe') return Number.MAX_SAFE_INTEGER + 1;
              throw new Error('unavailable');
            },
          }),
    };
    const registry = new Registry();
    const [machine, up] = pipe();
    const closed = new Promise<number>((resolve) => machine.listen({ frame: () => {}, close: resolve }));
    registry.bind('s', up, governance, log);
    machine.send({ kind: 'text', data: JSON.stringify(frame().message) });
    assert.equal(await closed, 1011);
    assert.throws(() => registry.control('s', null), { code: 'no_session' });
    assert.equal(replays, failure === 'replay' ? 1 : 0);
    assert.equal(appends, 0);
  });
}
