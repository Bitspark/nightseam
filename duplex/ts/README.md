# The seam, in TypeScript

`@nightseam/duplex` is the frames duplex connection beneath every protocol:
`FrameConnection` — ordered frames, both ways, an explicit close with a code
and a reason, and nothing else — and `webSocketConnection`, the one
transport today, which adapts a browser or Node `WebSocket` to it. The close
codes are the WebSocket registry's numbers on every transport, so that a
close means the same thing whatever carried it.

`@nightseam/runtime` speaks the `nightseam.duplex/1` profile over a
`FrameConnection` and never touches a WebSocket itself; a tunnel channel or
an in-memory pipe is a `FrameConnection` too, and the peer runs over either
unchanged. This package is the TypeScript half of `duplex/go`, the same seam
in Go.
