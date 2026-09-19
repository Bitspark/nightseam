# @nightseam/tunnel

```sh
npm install @nightseam/tunnel
```

Channels multiplexed over one peer of the `nightseam.duplex/1` profile.
Either side opens a channel, naming the family it will speak and the last
sequence it holds; each channel is a `FrameConnection` of `@nightseam/duplex`,
so a `DuplexPeer` — and every client Nightseam generates — runs over it
unchanged, and a handle in a family's message, `{"channel": 12}`, names one.

```ts
import { DuplexPeer } from '@nightseam/runtime';
import { Tunnel } from '@nightseam/tunnel';

const outer = new DuplexPeer();
await outer.connect('wss://example.test/hall');
const tunnel = new Tunnel(outer);

const channel = await tunnel.open('chat', 0);      // this side opens
const inner = new DuplexPeer();
await inner.attach(channel);                        // and speaks the profile over it

const accepted = await tunnel.accept();             // or takes what the other side opened
const named = tunnel.channel(handle.channel);       // or resolves a handle it was given
```

The outer peer sees four operations — `channel.open`, a request;
`channel.frame`, `channel.credit` and `channel.close`, events — and never
what a channel carries. Ids are the opener's, odd for the client of the
outer connection and even for its server. Flow control is per channel by
credit: a window each side declares at open and returns as it takes frames,
so a channel whose consumer stalls stalls its own sender and nothing else;
a send beyond the window waits in the channel and `buffered` counts it. A
frame over `maxFrameBytes` is refused before delivery and the channel with
it; a close carries its code and reason across; the outer connection closing
ends every channel with 1001. What arrives before anyone listens is held and
delivered, in order, to the first listener.

A tunnel takes no observer of its own: it emits through the observer of the
peer it runs over, declaring `channel.opened`, `channel.accepted`,
`channel.closed`, `credit.stall` and `open.refused` into the runtime's
`ObserverEvents` so that a consumer's `switch (event.type)` covers them
beside the runtime's. A layer built over a channel reaches the same observer
with `channel.observe(event)`.

The Go tunnel and this one are held to each other over a real socket. The
full description is [docs/tunnel.md](https://github.com/Bitspark/nightseam/blob/main/docs/tunnel.md), and
[docs/observability.md](https://github.com/Bitspark/nightseam/blob/main/docs/observability.md) has every
event of every layer.

Apache-2.0, with `NOTICE` beside it. The repository is
[Bitspark/nightseam](https://github.com/Bitspark/nightseam).
