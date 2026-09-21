// Exercises actual Nightseam carriers against Bitwire v0.2.0's composition oracle.
// Apache-2.0; Bitspark. Expected observations remain in the published Bitwire module.
import { once } from 'node:events';
import { WebSocket, WebSocketServer } from 'ws';
import type { Endpoint, Message, Path, Receiver, ReturnAddress, Wire } from '@bitspark/bitwire';
import { at, mount, webSocketConnection, type WebSocketLike } from '@nightseam/duplex';
import { forwardWire, wirePair, DuplexPeer, createDispatcher } from '@nightseam/runtime';

// ws implements the browser event surface used by the public duplex adapter;
// its overloaded addEventListener declaration has a different TypeScript shape.
const asLike = (socket: WebSocket): WebSocketLike => {
  socket.binaryType = 'arraybuffer';
  return socket as unknown as WebSocketLike;
};

const carrier = process.env.NIGHTSEAM_BITWIRE_CARRIER;
if (carrier !== 'local' && carrier !== 'peer') throw new Error('Expected local or peer carrier.');
const cleanups: (() => void)[] = [];

async function physicalPair(): Promise<[Endpoint, Endpoint]> {
  if (carrier === 'local') return wirePair();
  const listener = new WebSocketServer({ port: 0, host: '127.0.0.1' });
  cleanups.push(() => {
    listener.close();
  });
  await once(listener, 'listening', { signal: AbortSignal.timeout(5_000) });
  const address = listener.address();
  if (!address || typeof address === 'string') throw new Error('No WebSocket address.');
  const accepted = once(listener, 'connection', { signal: AbortSignal.timeout(5_000) });
  // Both awaits can fail independently; retain a rejection handler while the
  // outgoing socket's handshake is pending, then await this same promise.
  accepted.catch(() => {});
  listener.on('connection', (socket) => {
    cleanups.push(() => {
      socket.terminate();
    });
  });
  const client = new WebSocket(`ws://127.0.0.1:${address.port}`);
  cleanups.push(() => {
    client.terminate();
  });
  await once(client, 'open', { signal: AbortSignal.timeout(5_000) });
  const [remote] = (await accepted) as [WebSocket];
  const a = new DuplexPeer({ role: 'client' });
  const b = new DuplexPeer({ role: 'server' });
  cleanups.push(() => {
    a.close();
    b.close();
  });
  await Promise.all([a.attach(webSocketConnection(asLike(client))), b.attach(webSocketConnection(asLike(remote)))]);
  return process.env.NIGHTSEAM_BITWIRE_REVERSE === '1' ? [b.wire(), a.wire()] : [a.wire(), b.wire()];
}

// Observations contain no oracle values. All delivery, matching, attachment,
// correlation and closure below use the production runtime and duplex package.
const event: Message = { frame: { version: 1, kind: 'event', data: null } };
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return {
    resolve,
    promise: Promise.race([
      promise,
      new Promise<never>((_, reject) => {
        const timer = setTimeout(() => reject(new Error('Delivery deadline exceeded.')), 5_000);
        void promise.then(() => clearTimeout(timer));
      }),
    ]),
  };
}
function refused(action: () => unknown): boolean {
  try {
    action();
    return false;
  } catch {
    return true;
  }
}
function observe(
  endpoint: Endpoint,
  hooks: { send?: (path: Path, message: Message) => void; receive?: (path: Path, message: Message) => void },
): Endpoint {
  return {
    send(path, message) {
      hooks.send?.(path, message);
      endpoint.send(path, message);
    },
    receive(receiver) {
      return endpoint.receive({
        ...receiver,
        message(path, message) {
          hooks.receive?.(path, message);
          return receiver.message?.(path, message);
        },
      });
    },
    close: (code, reason) => endpoint.close(code, reason),
  };
}
async function ownedPair(): Promise<[Endpoint, Endpoint]> {
  const pair = await physicalPair();
  cleanups.push(() => {
    pair[0].close();
    pair[1].close();
  });
  return pair;
}
function respond(message: Message, result: unknown): void {
  if (message.frame.kind !== 'request' || !message.return) throw new Error('Expected admitted request.');
  message.return.wire.send([], { frame: { version: 1, kind: 'response', id: message.frame.id, result } });
}
async function sendObserved(wire: Wire, path: Path, delivered: ReturnType<typeof deferred<void>>): Promise<void> {
  wire.send(path, event);
  await delivered.promise;
}

async function siblings() {
  const [client, server] = await ownedPair();
  const router = createDispatcher(server);
  const deliveries: string[] = [];
  let signal = deferred<void>();
  let sending = false,
    receiverRanDuringSend = false;
  const receiver = (name: string): Receiver => ({
    message(path) {
      receiverRanDuringSend ||= sending;
      deliveries.push(`${name}:${path.join('/')}`);
      signal.resolve();
    },
  });
  const detachA = router.select(['a']).receive(receiver('a'));
  router.select(['b']).receive(receiver('b'));
  const duplicateEndpointAttachmentRefused = refused(() => server.receive({}));
  sending = true;
  at(client, ['a']).send(['run'], event);
  sending = false;
  await signal.promise;
  signal = deferred<void>();
  await sendObserved(at(client, ['b']), ['run'], signal);
  detachA();
  detachA();
  signal = deferred<void>();
  at(client, ['a']).send(['ignored'], event);
  await sendObserved(at(client, ['b']), ['after-detach'], signal);
  router.close();
  signal = deferred<void>();
  let attachmentReusableAfterDetach = false;
  const replacement = server.receive({
    message() {
      attachmentReusableAfterDetach = true;
      signal.resolve();
    },
  });
  router.close();
  await sendObserved(client, ['replacement'], signal);
  replacement();
  return { deliveries, receiverRanDuringSend, duplicateEndpointAttachmentRefused, attachmentReusableAfterDetach };
}
async function overlap() {
  const [client, server] = await ownedPair();
  const router = createDispatcher(server),
    deliveries: string[] = [];
  let signal = deferred<void>();
  router.select(['a']).receive({
    message(path) {
      deliveries.push(`parent:${path.join('/')}`);
      signal.resolve();
    },
  });
  const detach = router.select(['a', 'b']).receive({
    message(path) {
      deliveries.push(`deep:${path.join('/')}`);
      signal.resolve();
    },
  });
  const duplicateRouteRefused = refused(() => router.select(['a']).receive({}));
  await sendObserved(client, ['a', 'b', 'run'], signal);
  detach();
  signal = deferred<void>();
  await sendObserved(client, ['a', 'b', 'run'], signal);
  router.close();
  return { deliveries, duplicateRouteRefused };
}
async function composition() {
  const [caller, inbound] = await ownedPair(),
    [outbound, destination] = await ownedPair();
  let entered: Message | undefined, arrived: Message | undefined;
  const associated = new WeakMap<ReturnAddress, object>(),
    marker = {};
  let framePreserved = true,
    returnIdentityPreserved = true,
    associatedContextPreserved = false;
  const left = observe(inbound, {
    receive(_path, message) {
      entered = message;
    },
  });
  const right = observe(outbound, {
    send(_path, message) {
      framePreserved &&= message === entered && message.frame === entered?.frame;
      returnIdentityPreserved &&= message.return !== undefined && message.return === entered?.return;
    },
  });
  const detach = forwardWire(left, right);
  const router = createDispatcher(
    observe(destination, {
      receive(_path, message) {
        arrived = message;
        if (message.return) associated.set(message.return, marker);
      },
    }),
  );
  const view = router.select(['a']).select(['b']);
  const captured = deferred<Message>();
  let deliveredPath: Path = [];
  const stop = view.receive({
    message(path, message) {
      deliveredPath = [...path];
      framePreserved &&= message === arrived && message.frame === arrived?.frame;
      returnIdentityPreserved &&= message.return !== undefined && message.return === arrived?.return;
      associatedContextPreserved = !!message.return && associated.get(message.return) === marker;
      captured.resolve(message);
    },
  });
  const result = deferred<unknown>();
  const original: Message = {
    frame: {
      version: 1,
      kind: 'request',
      id: 'c:1',
      params: { nested: [null, 42, 'value'] },
      traceparent: '00-0123456789abcdef0123456789abcdef-0123456789abcdef-01',
      tracestate: 'vendor=opaque',
      meta: { key: 'value' },
    },
    return: {
      wire: {
        send(_path, message) {
          if (message.frame.kind !== 'response' || message.frame.error) throw new Error('Expected successful reply.');
          result.resolve(message.frame.result);
        },
      },
    },
  };
  const callerRouter = createDispatcher(
    observe(caller, {
      send(_path, message) {
        framePreserved &&= message === original && message.frame === original.frame;
        returnIdentityPreserved &&= message.return === original.return;
      },
    }),
  );
  const mounted = mount(new Map([['', callerRouter.select(['a']).select(['b'])]]));
  const composed = at(mounted, ['']);
  if ('receive' in composed || 'close' in composed) throw new Error('Selected access grants ownership.');
  composed.send(['run', ''], original);
  const request = await captured.promise;
  detach();
  detach();
  stop();
  view.close();
  mounted.close();
  callerRouter.close();
  respond(request, 'answer');
  const reply = await result.promise;
  let sourceStillUsable = false,
    targetStillUsable = false;
  let signal = deferred<void>();
  const stopSource = inbound.receive({
    message() {
      sourceStillUsable = true;
      signal.resolve();
    },
  });
  await sendObserved(caller, ['probe'], signal);
  stopSource();
  signal = deferred<void>();
  router.select(['probe']).receive({
    message() {
      targetStillUsable = true;
      signal.resolve();
    },
  });
  await sendObserved(outbound, ['probe'], signal);
  router.close();
  return {
    deliveredPath,
    framePreserved,
    returnIdentityPreserved,
    associatedContextPreserved,
    reply,
    borrowedEndpointUsableAfterDetach: sourceStillUsable && targetStillUsable,
  };
}
async function opaquePaths() {
  const [client, server] = await ownedPair();
  const router = createDispatcher(server),
    result: string[] = [];
  for (const [path, name] of [
    [[''], 'empty'],
    [['a/b'], 'slash'],
    [['a', 'b'], 'split'],
    [['é'], 'composed'],
    [['e\u0301'], 'decomposed'],
  ] as const) {
    const signal = deferred<void>();
    router.select(path).receive({
      message() {
        result.push(name);
        signal.resolve();
      },
    });
    await sendObserved(client, path, signal);
  }
  router.close();
  return result;
}
async function selectedEndpoints() {
  const [client, root] = await ownedPair(),
    router = createDispatcher(root);
  const a = router.select(['scope']).select(['a']),
    b = router.select(['scope', 'b']),
    detached = router.select(['detached']);
  const notifications = { active: 0, detached: 0, sibling: 0 };
  let nestedPath: Path = [],
    selectedSendPath: Path = [],
    siblingAfterViewClose = false,
    routeReusable = false;
  const initial = a.receive({
    message() {
      throw new Error('Detached receiver ran.');
    },
  });
  const duplicateReceiveRefused = refused(() => a.receive({}));
  initial();
  initial();
  let signal = deferred<void>();
  const rootClosed = deferred<void>();
  a.receive({
    message(path) {
      nestedPath = [...path];
      signal.resolve();
    },
    closed() {
      notifications.active++;
    },
  });
  initial();
  b.receive({
    message() {
      siblingAfterViewClose = true;
      signal.resolve();
    },
    closed() {
      notifications.sibling++;
      rootClosed.resolve();
    },
  });
  const unused = detached.receive({
    closed() {
      notifications.detached++;
    },
  });
  unused();
  detached.close();
  detached.close();
  client.receive({
    message(path) {
      selectedSendPath = [...path];
      signal.resolve();
    },
  });
  await sendObserved(client, ['scope', 'a', 'in'], signal);
  signal = deferred<void>();
  await sendObserved(a, ['out'], signal);
  a.close();
  a.close();
  const closedReceiveRefused = refused(() => a.receive({})),
    closedSendRefused = refused(() => a.send(['ignored'], event));
  signal = deferred<void>();
  const stop = router.select(['scope', 'a']).receive({
    message() {
      routeReusable = true;
      signal.resolve();
    },
  });
  await sendObserved(client, ['scope', 'a', 'replacement'], signal);
  signal = deferred<void>();
  await sendObserved(client, ['scope', 'b', 'sibling'], signal);
  stop();
  root.close();
  root.close();
  await rootClosed.promise;
  const rootReceiveRefused = refused(() => root.receive({})),
    viewAfterRootCloseRefused = refused(() => b.receive({}));
  b.close();
  return {
    nestedPath,
    selectedSendPath,
    duplicateReceiveRefused,
    notifications,
    closedReceiveRefused,
    closedSendRefused,
    rootReceiveRefused,
    viewAfterRootCloseRefused,
    siblingAfterViewClose,
    routeReusable,
  };
}
async function sameIDDelayedReplies() {
  const [client, root] = await ownedPair();
  let admitted: ReturnAddress | undefined;
  const router = createDispatcher(
    observe(root, {
      receive(_path, message) {
        admitted = message.return;
      },
    }),
  );
  const view = router.select(['service']),
    requests: Message[] = [],
    capturedReturnIdentities: boolean[] = [];
  let signal = deferred<void>();
  const stop = view.receive({
    message(_path, message) {
      requests.push(message);
      capturedReturnIdentities.push(message.return !== undefined && message.return === admitted);
      signal.resolve();
    },
  });
  const lateReplies: string[] = [],
    replyIDs: string[] = [];
  let replied = deferred<void>();
  for (const [name, payload] of [
    ['left', 'first'],
    ['right', 'second'],
  ]) {
    const reply: Wire = {
      send(_path, message) {
        if (message.frame.kind !== 'response') throw new Error('Expected reply.');
        lateReplies.push(`${name}:${message.frame.result}`);
        replyIDs.push(message.frame.id);
        replied.resolve();
      },
    };
    signal = deferred<void>();
    at(client, ['service']).send(['call'], {
      frame: { version: 1, kind: 'request', id: 'c:1', params: payload },
      return: { wire: reply },
    });
    await signal.promise;
  }
  stop();
  view.close();
  const replacementDeliveries: string[] = [];
  signal = deferred<void>();
  router.select(['service']).receive({
    message(_path, message) {
      if (message.frame.kind !== 'event') throw new Error('Old request reached replacement.');
      replacementDeliveries.push(String(message.frame.data));
      signal.resolve();
    },
  });
  client.send(['service', 'probe'], { frame: { version: 1, kind: 'event', data: 'probe' } });
  await signal.promise;
  for (const request of requests.reverse()) {
    replied = deferred<void>();
    if (request.frame.kind !== 'request') throw new Error('Expected request.');
    respond(request, request.frame.params);
    await replied.promise;
  }
  router.close();
  return { capturedReturnIdentities, lateReplies, replyIDs, replacementDeliveries };
}
try {
  const observations = {
    siblings: await siblings(),
    overlap: await overlap(),
    composition: await composition(),
    opaquePaths: await opaquePaths(),
    selectedEndpoints: await selectedEndpoints(),
    sameIDDelayedReplies: await sameIDDelayedReplies(),
  };
  process.stdout.write(`${JSON.stringify(observations)}\n`);
} finally {
  for (const close of cleanups.reverse()) close();
}
