# The tunnel

A tunnel multiplexes channels over one peer of the profile. Either side
opens a channel, saying the family it speaks and the last sequence it holds;
each channel is a connection of the seam — `duplex.Conn` in Go,
`FrameConnection` in TypeScript — held to the same conformance suite as the
WebSocket and the pipe, so a peer of any family runs over it unchanged, and
a handle in a family's message, `{"channel": 12}`, names one. The outer peer
sees four operations of the profile's own and never what a channel carries.

`tunnel/go` and `@nightseam/tunnel` are held to each other over a real
socket in both directions.

## The operations

All four are methods and events on the outer peer, in the profile's own
vocabulary.

**`channel.open`** — a request, from the side that opens:

```json
{"channel": 12, "family": "chat", "after": 0, "window": 32}
```

`channel` is the id the opener chose (§ ids), `family` the family the
channel will speak, `after` the last sequence the opener holds — carried,
never acted on, for whatever runs above to resume from — and `window` how
many frames the opener will hold in flight from the other side before it
returns credit. The result is `{"window": 32}`, the accepting side's
window. An open is refused with `channel_invalid` (a malformed open, or an
id of the accepting side's parity), `channel_exists` (the id is open) or
`channel_refused` (nobody here has taken the channels already opened, up
to the accept capacity).

**`channel.frame`** — an event, either way, one inner frame:

```json
{"channel": 12, "text": "{\"version\":1,\"kind\":\"request\",…}"}
{"channel": 12, "binary": "AQID/w=="}
```

Exactly one of `text` and `binary`; a binary frame is base64. The inner
frame is opaque to the tunnel: a text is a text, whatever it says. A frame
for a channel that is not open is dropped.

**`channel.credit`** — an event, from the receiver: `{"channel": 12,
"frames": 16}`, returning credit for frames its consumer took.

**`channel.close`** — an event, from the side that closes: `{"channel": 12,
"code": 1000, "reason": "session ended"}`. It travels in order behind the
frames sent before it, so what was sent before a close is delivered before
the close is.

## Ids

A channel's id is chosen by the side that opens it: **odd** for the client
of the outer connection, **even** for its server. So both sides may open
without a race or a collision, and an id names one channel of one outer
connection whichever side opened it; a handle carried in a family's message
refers to the connection that carries the message.

## Credit

Flow control is per channel, by credit. Each side may have at most a window
of frames in flight to the other on a channel — the window the other side
declared at open — and a receiver returns credit as its consumer takes
frames, in halves of the window. A `Send` beyond the window waits until
credit arrives or its context ends; in TypeScript the frame waits in the
channel and `buffered` counts it, so a peer above paces on it as on any
connection.

The reason it is per channel: the outer peer ends a connection whose events
it cannot deliver, so a stalled channel may never be the outer peer's to
buffer — it stalls its own sender and nothing else.

## Limits and closes

- A received inner frame larger than the tunnel's `MaxFrameBytes` is
  refused before delivery and the channel with it, closed to the other side
  with 1009; the outer peer's own limit bounds the event that carries a
  frame.
- A frame beyond the window, or one that is neither text nor binary, ends
  the channel with 1002.
- `Close` carries its code and reason across; `Abort` ends the channel at
  once and the other side sees 1006, as it would a dropped transport.
- The outer connection closing ends every channel with 1001, going away,
  and each side's connection sees it.
- What arrives before anyone receives is held, one window deep in both: in
  Go a channel's inbox is a window's frames, in TypeScript a channel holds as
  many until the first listener and delivers and credits them then, and a
  frame arriving beyond them is the refusal above rather than a queue that
  grows to the sender's choosing. A close that arrived behind them comes
  last, the refusal of the frame that overran the window among them.
- A channel the other side opened is taken by `Accept` or by resolving its
  id (`Channel(id)` in Go, `channel(id)` in TypeScript — which is what the
  generated `Open` does with a handle); channels nobody has taken are bounded
  by the accept capacity, 64 by default, and an open beyond it is refused.

## Options

| option | default | meaning |
| --- | --- | --- |
| `MaxFrameBytes` | 1 MiB | bound on a received inner frame |
| `Window` | 32 | frames the other side may have in flight on a channel |
| `AcceptCapacity` | 64 | channels the other side may have opened that nobody here took |

## Using it

```go
carrier, _ := tunnel.New(peer, tunnel.Options{})       // once per outer peer
channel, _ := carrier.Open(ctx, "chat", 0)             // this side opens
served, _ := chatbinding.Serve(ctx, channel, …)        // and speaks the family over it

accepted, _ := carrier.Accept(ctx)                     // or takes what the other side opened
client, _ := chatclient.Open(ctx, carrier, handle, …)  // or resolves a handle it was given
```

```ts
const tunnel = new Tunnel(peer);
const channel = await tunnel.open('chat');
const client = await Client.attach(channel, …);
const resolved = await Client.open(tunnel, handle, …);
```

## Observing it

A tunnel takes no observer of its own: it emits through the observer of the
peer it runs over, as `ChannelOpened`, `ChannelAccepted`, `ChannelClosed`,
`CreditStall` and `OpenRefused` — in TypeScript `channel.opened`,
`channel.accepted`, `channel.closed`, `credit.stall` and `open.refused`,
declared into the runtime's `ObserverEvents`. A layer built over a channel
reaches that observer the same way, through `Channel.Peer()` in Go and
`channel.observe(event)` in TypeScript.
[observability.md](observability.md) has the rule and every event of every
layer.
