# @nightseam/duplex

The seam beneath every protocol, in TypeScript: `FrameConnection` — ordered
frames, both ways, an explicit close with a code and a reason, and nothing
else — with `webSocketConnection`, which adapts a browser or Node `WebSocket`
to it, and `pipe()`, two connected ends in memory for tests. The close codes
are the WebSocket registry's numbers on every transport, so that a close
means the same thing whatever carried it.

```ts
import { webSocketConnection, pipe } from '@nightseam/duplex';
import type { FrameConnection } from '@nightseam/duplex';

const connection: FrameConnection = webSocketConnection(new WebSocket(url));
const [left, right] = pipe();
```

`@nightseam/runtime` speaks the `nightseam.duplex/1` profile over a
`FrameConnection` and never touches a WebSocket itself; a tunnel channel or
an in-memory pipe is a `FrameConnection` too, and the peer runs over either
unchanged. This package is the TypeScript half of the seam; `duplex/go` in
the repository is the Go half, and each is held to the conformance suite of
its language — `src/conformance.ts` here, `duplex/go/duplextest` there —
which the pipe, the WebSocket adapter and a tunnel channel all run.
