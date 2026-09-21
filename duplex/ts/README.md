# @nightseam/duplex

```sh
npm install @nightseam/duplex
```

The common structured access surface is `Wire`: `send(path, message)`,
`receive(path, receiver)` and `close(code, reason)`. Paths are arrays of
Unicode strings; a message carries one of the four profile frame kinds and
an optional local return capability, never another serialized envelope.

These types are re-exported from the public `@bitspark/bitwire@0.1.0`
contract. The dependency installs with this package; routing views, codecs,
recording and transports remain Nightseam implementations.

```ts
import { at, mount } from '@nightseam/duplex';
import type { Wire } from '@nightseam/duplex';

const joined: Wire = mount(new Map([['work', workWire], ['chat', chatWire]]));
const selected = at(joined, ['work']);
```

Selection and mounting reuse existing roots, with no peer or channel allocated
even on first use. Receivers match exact paths by default; `namespace: true`
also matches descendants, with exact matches winning and otherwise the longest
segment prefix. Closing a selected view closes its root; closing a mount
detaches its registrations and leaves child roots open. The runtime supplies
bounded asynchronous roots as `peer.wire()` and `wirePair()`.

The raw transport seam remains `FrameConnection` — ordered
frames, both ways, an explicit close with a code and a reason, and nothing
else — with `webSocketConnection`, which adapts a browser or Node `WebSocket`
to it, and `pipe()`, two connected ends in memory for tests. The close codes
are the WebSocket registry's numbers on every transport, so that a close
means the same thing whatever carried it.

A transport carries backpressure: `buffered` is what a send left with it and
it has not taken — a socket's bytes, a pipe's frames — and a peer sends the
next frame only once it reads zero. The pipe holds eight frames in flight per
direction, as the Go pipe does; a send past that is held until the far end
takes one, so a peer paced over a pipe is paced as it is over a socket.

```ts
import { webSocketConnection, pipe } from '@nightseam/duplex';
import type { FrameConnection } from '@nightseam/duplex';

const connection: FrameConnection = webSocketConnection(new WebSocket(url));
const [left, right] = pipe();
```

`@nightseam/runtime` speaks the `nightseam.duplex/1` profile over a
`FrameConnection` and never touches a WebSocket itself; a tunnel's raw
`Connection` or an in-memory pipe is a `FrameConnection` too, and the peer runs over either
unchanged. This package is the TypeScript half of the seam;
[`duplex/go`](https://github.com/Bitspark/nightseam/tree/main/duplex/go) is the Go half, and each is held
to the conformance suite of its language —
[`src/conformance.ts`](https://github.com/Bitspark/nightseam/blob/main/duplex/ts/src/conformance.ts) here,
[`duplex/go/duplextest`](https://github.com/Bitspark/nightseam/tree/main/duplex/go/duplextest) there —
which the pipe, the WebSocket adapter and a tunnel channel all run.

Apache-2.0, with `NOTICE` beside it. The repository is
[Bitspark/nightseam](https://github.com/Bitspark/nightseam).
