# @nightseam/tunnel

```sh
npm install @nightseam/tunnel
```

Channels multiplexed over one peer of `nightseam.duplex/1`. Each acquired
`Channel` implements `Endpoint`: it has `send(path, message)`,
`receive(receiver)` — one owning attachment — and `close(code, reason)`. A
generated model can interpret that Wire, select a path under it or mount it
beside another endpoint.

```ts
import { DuplexPeer, forwardWire } from '@nightseam/runtime';
import { Tunnel } from '@nightseam/tunnel';

const outer = new DuplexPeer();
const tunnel = new Tunnel(outer);
await outer.connect('wss://example.test/hall');

// modelWire was constructed with either generated side's toWire(model, context).
const channel = await tunnel.open('chat', {
  prepare: inner => { forwardWire(inner.wire(), modelWire); },
});
const named = await tunnel.channel(channel.id); // the same prepared Channel
```

`open(family, options)`, `accept(options)` and `channel(id, options)` acquire
one inner peer eagerly. The first acquisition owns the options; repeated
lookup returns the same Wire. `prepare` completes synchronously before that
inner peer starts reading. Path selection and mounting add no peer, including
on first use. The host owns the lifetime of the local model Wire as well as the
physical channel's.

The raw `FrameConnection` surface is separately named `Connection`, reached
with `openConnection(family)`, `acceptConnection()` and `connection(id)`.
A connection can have one reader presentation: raw or prepared Wire.
Use raw access for non-profile protocols or transport-level work.

The outer peer sees four operations: `channel.open`, `channel.frame`,
`channel.credit` and `channel.close`. Channel ids are odd for the outer
client and even for its server. Per-channel credit bounds frames in flight;
a stalled channel paces its own sender. Raw `buffered` counts frames waiting
for credit. A prepared channel's inner peer consumes those frames and applies its
own bounded queues. A frame over the tunnel's `maxFrameBytes` ends the channel;
outer closure ends all channels with 1001.

The tunnel emits `channel.opened`, `channel.accepted`, `channel.closed`,
`credit.stall` and `open.refused` through its outer peer's observer.
Choose inner peer observation through acquisition options and generated model
observation through the adapter context.

The paired surface is documented in
[docs/runtime/tunnel.md](https://github.com/Bitspark/nightseam/blob/main/docs/runtime/tunnel.md),
and the unchanged outer vocabulary in
[docs/wire/tunnel.md](https://github.com/Bitspark/nightseam/blob/main/docs/wire/tunnel.md).

Apache-2.0, with `NOTICE` beside it. The repository is
[Bitspark/nightseam](https://github.com/Bitspark/nightseam).
