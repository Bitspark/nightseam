# @nightseam/duplex

```sh
npm install @nightseam/duplex
```

The common structured access contract comes directly from `@bitspark/bitwire@0.2.0`.
`Wire` supplies `send(path, message)`; `Endpoint` adds `receive(receiver)` and
`close(code, reason)`. Import shared types from Bitwire and composition helpers
from Nightseam.

```ts
import type { Endpoint, Wire } from '@bitspark/bitwire';
import { at, mount } from '@nightseam/duplex';

declare const work: Endpoint, chat: Endpoint;
const joined: Endpoint = mount(new Map([['work', work], ['chat', chat]]));
const selected: Wire = at(joined, ['work']);
```

Paths contain opaque Unicode strings; messages contain a profile frame and an
optional local return capability. Selection is send-only. A mount takes one
attachment per child and restores the consumed segment on delivery. Closing a
mount detaches its attachments and leaves borrowed children open.

The runtime supplies `peer.wire()`, `wirePair()` and `createDispatcher(endpoint)`.
A dispatcher owns one endpoint attachment, performs exact and prefix matching,
and provides receiving selections through `dispatcher.select(path)`.

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
