# The profile: `nightseam.duplex/1`

The profile is what a peer speaks over a connection of the seam: JSON text
frames, each one envelope, carrying requests, responses, events and
cancellations both ways. It is what every generated package binds to and
what the runtimes implement — `runtime/go` and `@nightseam/runtime` — held
to each other over a real socket. A consumer speaks it through the generated
code and rarely needs this page; a runtime for another language needs
nothing else.

## The connection beneath

A frames duplex connection: ordered, message-framed, bidirectional, closed
explicitly with a code and a reason, and nothing else — `duplex.Conn` in Go,
`FrameConnection` in TypeScript. A WebSocket is one; an in-memory pipe and a
tunnel channel are others; every transport is held to one conformance suite
(`duplex/go/duplextest`). Close codes are the WebSocket registry's numbers on
every transport: 1000 normal, 1001 going away, 1002 protocol error, 1006
abnormal closure, 1008 policy violation, 1009 too large, 1011 internal, and
4000–4999 for what runs above the seam; the profile itself closes with
**4011** when the other side broke it.

The profile sends text frames only and refuses a binary frame; a frame
larger than the peer's limit is refused before delivery and the connection
with it. The seam frames every frame whole; the profile never splits one.

## The envelope

One JSON object per frame, with `version` `1` and a `kind`:

| kind | members | rule |
| --- | --- | --- |
| `request` | `id`, `method`, `params` | `params` is present, `{}` where the method takes none |
| `response` | `id`, exactly one of `result` or `error` | `error` is `{code, message, data?}` with `code` and `message` non-empty |
| `event` | `event`, `data` | `data` is present, `null` where there is none |
| `cancel` | `id` | the request it withdraws |

Every kind may carry `traceparent` and `tracestate` (§ below). No other
member is allowed; a member of another kind, a duplicate member, an unknown
member or trailing content after the object makes the frame malformed. A
malformed frame ends the connection — a peer that sends one is not a peer
to keep talking to — with 4011.

Payloads — `params`, `result`, `error.data`, `data` — are any JSON value
the family declares; the profile carries them and the generated validators
read them. A number the receiver cannot represent exactly (beyond
JavaScript's safe integers, non-finite) is refused by the validators, not
by the profile.

## Ids and correlation

A request's `id` is minted by the side that sends it, with that side's
prefix: `c:` for the client of the connection, `s:` for the server, followed
by a decimal without leading zeros of at most twenty digits: `c:1`, `s:42`.
The prefix is the role's, so the two sides never mint the same id; a
response carries the id of the request it answers, a cancel the id of the
request it withdraws, and an id with the wrong prefix for its kind is
malformed.

Ids are per connection. A relay that carries frames across connections mints
its own on the way out and maps the responses back (`docs/session.md`).

## Requests

Either side may send a request at any time; requests are concurrent and a
response may arrive in any order. A request is answered exactly once, with a
`result` or an `error`. The runtime answers on the handler's behalf where
the handler does not:

| error code | when |
| --- | --- |
| `method_not_found` | no handler for `method` |
| `busy` | the receiver has as many requests open as it allows |
| `cancelled` | the request was withdrawn, or its deadline passed, before the handler answered |
| `internal` | the handler failed with an error that is not public, or panicked; the message says nothing more |

A handler's own error crosses the wire only when it is a public one —
`runtime.PublicError` in Go, `DuplexError` in TypeScript — with its `code`,
`message` and optional `data`; the family's declared errors are these, by
name, in the generated packages.

A **cancel** is best effort: it withdraws a request the receiver may already
have answered. The receiver cancels the handler's context; the handler's
response, if it still sends one, is `cancelled`. A cancel for an id that is
not open is ignored.

Every request has a deadline on the sender's side (30 seconds by default)
after which it is failed locally as `cancelled` and a cancel is sent; the
receiver's own deadline for a handler is the same.

## Events

An event carries a name and data, expects no answer, and is delivered in
the order it was sent. Handlers run one at a time in that order; an event
with no handler is dropped. An event says nothing about receipt: the
sender's `Emit` resolves when the frame was accepted for sending.

## Limits and backpressure

Every queue is bounded, per connection: 128 outgoing frames, 128 events
waiting for their handlers, 64 requests being handled at once, frames of at
most 1 MiB, by default. A producer that fills a queue is paced for one write
deadline (10 seconds); a consumer that still does not drain it is
disconnected rather than allowed to hold the connection up — a stalled
consumer ends the connection, in both runtimes.

## Trace context

Every kind of envelope may carry the two members of W3C Trace Context,
`traceparent` and `tracestate`, verbatim: `traceparent` is
`version-traceid-spanid-flags` in its lowercase hexadecimal form and is
refused in any other; `tracestate` has no form of its own and may travel
alone, since an intermediary may strip one member and not the other.

The runtimes propagate them and read nothing into them:

- an incoming request's or event's trace is placed on the context its
  handler runs with;
- a request or event sent from that context carries a **child**: the same
  version, trace id and flags under a span id of its own, with the
  `tracestate` forwarded verbatim; one sent from no context begins a trace
  of its own;
- a response carries its request's members byte for byte; a cancel the
  request's; a `tracestate` that arrived without a `traceparent` continues
  no trace and is carried back on the response.

The default propagator mints a random 16-byte trace id and 8-byte span id
and correlates with no tracing library installed; an adapter for one
replaces it (`Options.Propagator` in Go, `PeerOptions.propagator` in
TypeScript) and the runtime imports none.

## What the profile does not do

No retry, no reconnection, no acknowledgment of delivery, no
authentication: a connection is authenticated by whatever opened it — the
HTTP upgrade in Go takes an `Authenticate` hook — and the profile begins
once it is open. Resumption after a reconnect is a family's operation
(`after` on a tunnel's `channel.open`, a session's log), not the
profile's.

## A frame, on the wire

```json
{"version":1,"kind":"request","id":"c:7","method":"work.start","params":{"text":"hi"},
 "traceparent":"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}
{"version":1,"kind":"response","id":"c:7","result":{"work":"w-1"},
 "traceparent":"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}
{"version":1,"kind":"event","event":"work.changed","data":{"work":"w-1"},
 "traceparent":"00-4bf92f3577b34da6a3ce929d0e0e4736-9f4c5a2e1b3d7c60-01"}
{"version":1,"kind":"cancel","id":"c:8"}
```
