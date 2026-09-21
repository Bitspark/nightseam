# The tunnel's surface

`tunnel/go` and `@nightseam/tunnel` multiplex channels over one peer. A
`Channel` is a prepared `Wire`: generated models can interpret it directly,
select a relative path within it, or mount it beside another Wire. What
crosses the outer connection remains [the tunnel vocabulary](../wire/tunnel.md).

## Making one, and when

Make the tunnel before its outer peer reads the first frame. In Go use
`runtime.Options.Prepare`; in TypeScript construct `Tunnel` before `connect`
or `attach`, or use `PeerOptions.prepare`. This installs `channel.open` and
the three channel event handlers before the remote can use them. `OnConnect`
is too late for registration ([the peer](peer.md#when-a-peer-starts-reading)).

```go
var carrier *tunnel.Tunnel
peer, _, err := runtime.Dial(ctx, url, runtime.DialOptions{
    Options: runtime.Options{Prepare: func(p *runtime.Peer) (err error) {
        carrier, err = tunnel.New(p, tunnel.Options{})
        return err
    }},
})
```

```ts
const peer = new DuplexPeer();
const carrier = new Tunnel(peer);
await peer.connect(url);
```

## The surface

| Operation | Go | TypeScript |
| --- | --- | --- |
| open a prepared Wire | `carrier.Open(ctx, family, digest, runtime.Options{})` | `await carrier.open(family, digest, options)` |
| accept a prepared Wire | `carrier.Accept(ctx, runtime.Options{})` | `await carrier.accept(options)` |
| resolve a prepared Wire by id | `carrier.Channel(id, options)` → `(*Channel, bool, error)` | `await carrier.channel(id, options)` → `Channel \| undefined` |
| identify a channel | `channel.ID`, `channel.Family`, `channel.Digest` | `channel.id`, `channel.family`, `channel.digest` |
| use its Endpoint | `Send(path, message)`, `Receive(receiver)`, `Close(code, reason)` | `send`, `receive`, `close` |

A channel is an `Endpoint`: it has one owning attachment, and a dispatcher
composed over it holds whatever routing a consumer wants. Its inner peer is a
carrier of its own, so it mints its own request serials and maps the replies
back ([the profile](../wire/profile.md#serials-increase-in-publication-order)).

Acquisition constructs one inner peer eagerly. Its options and `Prepare` /
`prepare` install the model before that peer starts reading. Repeated lookup
returns the same channel; the first acquisition owns its options. `At` and
`Mount` work on that existing Wire and create no inner peer, even on first use.
The acquisition context bounds the wait; the Go inner peer lives with the
outer peer's context, so returning from the RPC that opened it does not end it.

Prepare the inner peer by forwarding its Wire to the local Wire returned by
either side's generated `ToWire` / `toWire`. Its factory receives the opposite
proxy, which the host may retain for calls after acquisition. TypeScript's
`prepare` must complete synchronously; do not await `fromWire` inside it. Carrier
assembly and lifetime belong to the host; the generated
[model factory](../declaration/generated.md) has the same type on either route.

### Raw connections

The lower frame transport is separate. `OpenConnection(ctx, family, digest)`,
`AcceptConnection(ctx)` and `Connection(id)` expose a Go `*Connection`
implementing `duplex.Conn`. TypeScript's `openConnection(family, digest)`,
`acceptConnection()` and `connection(id)` expose a `Connection` implementing
`FrameConnection`. Use these for a raw protocol or transport conformance work.

A connection can have one reader presentation: raw or prepared Wire. A raw
claim and a Wire claim conflict; selecting a path is not another claim. Raw
Go operations are `Send(ctx, frame)`, `Receive(ctx)`, `Close(ctx, code, reason)`
and `Abort()`. TypeScript exposes `send`, `listen`, `close`, `state` and
`buffered`. Close carries its code and reason across; raw abort ends the
channel immediately and the other side sees 1006.

## Options

| Go | TypeScript | default | meaning |
| --- | --- | --- | --- |
| `MaxFrameBytes` | `maxFrameBytes` | 1 MiB | bound on a received inner frame |
| `Window` | `window` | 32 | frames the other side may have in flight on a channel |
| `AcceptCapacity` | `acceptCapacity` | 64 | channels the other side opened that nobody here took |
| `Contracts` | `contracts` | empty map | known family names mapped to generated digests, copied when the tunnel is created |

Supply generated `WireDigest()` / `wireDigest` values for the families the
tunnel expects. A different nonempty incoming digest is refused
`contract_mismatch` before channel allocation or admission. An empty digest
argument represents no revision claim and is omitted on the wire; absence
on either side does not cause this refusal. Both raw connections and prepared
channels retain the opener's digest beside the family name. The digest check
is independent of which side opened the channel.

These are tunnel options. Each prepared channel also takes ordinary runtime
options for its inner peer's queue, pending-call, handler and frame bounds.

## Credit in each language

Credit remains per raw channel. Go's raw `Send` waits past the window until
credit arrives or its context ends. TypeScript holds the frame in the raw
connection and counts it in `buffered`, so its inner peer paces writes. Before
a reader takes the connection, it holds one window of frames. A prepared
channel's eager inner reader returns credit as it takes those frames, and its
own bounded runtime queues govern subsequent delivery. A stalled channel
stalls its own sender, not the outer peer's event loop.

## Observing it

The tunnel emits through its outer peer's observer: `ChannelOpened`,
`ChannelAccepted`, `ChannelClosed`, `CreditStall` and `OpenRefused` in Go;
`channel.opened`, `channel.accepted`, `channel.closed`, `credit.stall` and
`open.refused` in TypeScript. Inner peer observation is chosen through the
channel's runtime options; generated model observation is chosen through its
adapter context. [The observer](observer.md) lists each event and its fields.
