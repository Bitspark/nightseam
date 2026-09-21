# The tunnel's surface

`tunnel/go` and `@nightseam/tunnel` multiplex channels over one peer, each
channel a connection of the seam — `duplex.Conn` in Go, `FrameConnection`
in TypeScript — so a peer, and every client Nightseam generates, runs over
one unchanged. The two are held to each other over a real socket in both
directions. This page is the surface in both languages; what crosses the
wire is [the tunnel](../wire/tunnel.md).

## Making one, and when

A tunnel is made over a peer **before that peer reads its first frame**, on
whichever side may be opened to: `tunnel.New(peer, options)` registers
`channel.open` and the three channel events on the peer, and a peer that is
already reading can refuse the other side's first open `method_not_found`
before they are there. In Go that place is `Options.Prepare`, which runs on
the peer between its construction and its loops — `ServerOptions` and
`DialOptions` both reach it, and `OnConnect` is too late ([the
peer](peer.md#when-a-peer-starts-reading)). In TypeScript it is the ordering
the peer has anyway: make the `Tunnel`, then `connect` or `attach`.

```go
handler, _ := runtime.NewHandler(runtime.ServerOptions{
	Options: runtime.Options{Prepare: func(peer *runtime.Peer) error {
		carrier, err := tunnel.New(peer, tunnel.Options{}) // once per outer peer, before it reads
		if err != nil {
			return err
		}
		go serve(peer.Context(), carrier)                  // takes what the other side opens
		return nil
	}},
	Authenticate: authenticate, CheckOrigin: allow,
})
```

```go
var carrier *tunnel.Tunnel
peer, _, _ := runtime.Dial(ctx, url, runtime.DialOptions{
	Options: runtime.Options{Prepare: func(p *runtime.Peer) (err error) {
		carrier, err = tunnel.New(p, tunnel.Options{})     // the dialing side, likewise
		return err
	}},
})
defer peer.Close()
channel, _ := carrier.Open(ctx, "chat", chatprotocol.WireDigest()) // this side opens
served, _ := chatbinding.Serve(ctx, channel, …)        // and speaks the family over it

accepted, _ := carrier.Accept(ctx)                     // or takes what the other side opened
client, _ := chatclient.Open(ctx, carrier, handle, …)  // or resolves a handle it was given
```

```ts
const peer = new DuplexPeer();
const carrier = new Tunnel(peer);                      // before the peer is attached
await peer.connect(url);
const channel = await carrier.open('chat', wireDigest);
const client = await Client.attach(channel, …);
const resolved = await Client.open(carrier, handle, …);
```

## The surface

| | Go | TypeScript |
| --- | --- | --- |
| open a channel, naming the family and declaration digest it speaks | `carrier.Open(ctx, family, digest)` → `*Channel` | `carrier.open(family, digest)` |
| take a channel the other side opened | `carrier.Accept(ctx)` | `carrier.accept()` |
| resolve one by id — what the generated `Open` does with a handle | `carrier.Channel(id)` → `(*Channel, bool)` | `carrier.channel(id)` |
| the peer it runs over | `carrier.Peer()`, `channel.Peer()` | `channel.observe(event)` reaches its observer |
| a channel as a connection of the seam | `Send(ctx, frame)`, `Receive(ctx)`, `Close(ctx, code, reason)`, `Abort()` | `send`, `listen`, `close`, `state`, `buffered` |

`Close` carries its code and reason across; `Abort` ends the channel at
once and the other side sees 1006.

## Options

| Go | TypeScript | default | meaning |
| --- | --- | --- | --- |
| `MaxFrameBytes` | `maxFrameBytes` | 1 MiB | bound on a received inner frame |
| `Window` | `window` | 32 | frames the other side may have in flight on a channel |
| `AcceptCapacity` | `acceptCapacity` | 64 | channels the other side may have opened that nobody here took |
| `Contracts` | `contracts` | empty map | known family names mapped to generated digests, copied when the tunnel is created |

Supply generated `WireDigest()` / `wireDigest` values for the families the
tunnel expects. A different nonempty incoming digest is refused
`contract_mismatch` before the channel enters the accept queue. An empty
digest argument represents no revision claim and is omitted on the wire;
absence on either side does not cause this refusal. `Channel.Digest` /
`channel.digest` retains what the opener declared, beside `Family` /
`family`. The digest check is independent of which side opened the channel.

## Credit in each language

A `Send` beyond the window waits until credit arrives or its context ends;
in TypeScript the frame waits in the channel and `buffered` counts it, so a
peer above paces on it as on any connection. What arrives before anyone
receives is held one window deep in both: in Go a channel's inbox is a
window's frames, in TypeScript a channel holds as many until the first
listener and delivers and credits them then.

## Observing it

A tunnel takes no observer of its own: it emits through the observer of the
peer it runs over, as `ChannelOpened`, `ChannelAccepted`, `ChannelClosed`,
`CreditStall` and `OpenRefused` — in TypeScript `channel.opened`,
`channel.accepted`, `channel.closed`, `credit.stall` and `open.refused`,
declared into the runtime's `ObserverEvents`. A layer built over a channel
reaches that observer the same way, through `Channel.Peer()` in Go and
`channel.observe(event)` in TypeScript. [The observer](observer.md) has the
rule and every event of every layer.
