import assert from 'node:assert/strict';
import { at, Declared, refusingOrigin } from '@nightseam/duplex';
import { callWire, createDispatcher, DuplexError, handleWire, wirePair } from '@nightseam/runtime';

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

async function bounded<T>(promise: Promise<T>): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([
      promise,
      new Promise<never>((_, reject) => {
        timer = setTimeout(() => reject(new Error('timed out waiting for handler')), 10_000);
      }),
    ]);
  } finally {
    clearTimeout(timer);
  }
}

const [caller, callee] = wirePair();
const dispatcher = createDispatcher(callee);
const controller = new AbortController();
const started = deferred();
const ended = deferred();
handleWire(dispatcher, ['jobs', 'original'], async (_params, context) => {
  started.resolve();
  if (!context.signal.aborted) {
    await new Promise<void>((done) => context.signal.addEventListener('abort', () => done(), { once: true }));
  }
  ended.resolve();
  return null;
});
handleWire(dispatcher, ['jobs', 'replacement'], () => 'replacement');

try {
  let root = Declared.compose(refusingOrigin, [['run', at(caller, ['jobs', 'original'])]]);
  const captured = root.bind();
  // Attach the rejection handler immediately, including while waiting for dispatch.
  const pending = callWire(captured, ['run'], null, { signal: controller.signal, timeoutMs: 10_000 }).then(
    () => null,
    (error: unknown) => error,
  );
  await bounded(started.promise);

  // Assembler replacement affects fresh access; the pending call keeps its own.
  const { origin } = root.decompose();
  root = Declared.compose(origin, [['run', at(caller, ['jobs', 'replacement'])]]);
  controller.abort();
  const result = await bounded(pending);
  assert.ok(result instanceof DuplexError && result.code === 'cancelled');
  // Also observe cancellation at the original handler, not only at the caller.
  await bounded(ended.promise);
  console.log('original request: cancelled');

  const fresh = await callWire<string>(root.bind(), ['run'], null, { timeoutMs: 10_000 });
  assert.equal(fresh, 'replacement');
  console.log('new request:', fresh);
} finally {
  controller.abort();
  dispatcher.close();
  caller.close();
  callee.close();
}
