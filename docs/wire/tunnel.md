# The tunnel

A tunnel multiplexes channels over one peer of the profile. Either side
opens a channel, saying the family it speaks and the last sequence it holds;
each channel is a connection of the seam, held to the same conformance
suite as the WebSocket and the pipe, so a peer of any family runs over it
unchanged, and a handle in a family's message, `{"channel": 12}`, names one.
The outer peer sees four operations of the tunnel's own vocabulary and never
what a channel carries. This page is what crosses the wire; what a consumer
calls is [the tunnel's surface](../runtime/tunnel.md).

## The operations

All four are methods and events on the outer peer, ordinary frames of the
profile under the `channel.` prefix ([how a layer speaks](vocabulary.md)).

**`channel.open`** — a request, from the side that opens:

```json
{"channel": 12, "family": "chat", "window": 32}
```

`channel` is the id the opener chose (§ ids), `family` the family the
channel will speak, and `window` how many frames the opener will hold in
flight from the other side before it returns credit. The result is `{"window": 32}`, the accepting side's
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
frames, in halves of the window. A send beyond the window waits until
credit arrives; how each runtime makes a sender wait is [its
own](../runtime/tunnel.md#credit-in-each-language).

It is per channel so that a stalled channel stalls its own sender and
nothing else — the outer peer ends a connection whose events it cannot
deliver, so a stalled channel may never be the outer peer's to buffer
([credit is per channel](../decisions/credit-is-per-channel.md)).

## Limits and closes

- A received inner frame larger than the tunnel's frame limit (1 MiB by
  default) is refused before delivery and the channel with it, closed to
  the other side with 1009; the outer peer's own limit bounds the event
  that carries a frame.
- A frame beyond the window, or one that is neither text nor binary, ends
  the channel with 1002.
- A close carries its code and reason across; an abort ends the channel at
  once and the other side sees 1006, as it would a dropped transport.
- The outer connection closing ends every channel with 1001, going away,
  and each side's connection sees it.
- What arrives before anyone receives is held, one window deep, and a frame
  arriving beyond that is the refusal above rather than a queue that grows
  to the sender's choosing. A close that arrived behind them comes last, the
  refusal of the frame that overran the window among them.
- Channels the other side opened that nobody here has taken are bounded by
  the accept capacity, 64 by default, and an open beyond it is refused
  `channel_refused`.

## Observing it

A tunnel takes no observer of its own: it emits through the observer of the
peer it runs over, five events — a channel opened, accepted and closed, a
credit stall, an open refused — named for the family the channel was opened
for. [The observer](../runtime/observer.md) has the rule and every event of
every layer.
