# The profile: `nightseam.duplex/1`

The profile is what a peer speaks over a connection of the seam: JSON text
frames, each one envelope, carrying requests, responses, events and
cancellations both ways. It is what every generated package binds to and
what every language's runtime implements, each held to the reference over a
real socket by the conformance suite. This page is the wire — what a peer of
any language sends, accepts and refuses — and names no runtime: what a
consumer calls, in each language, is [the peer](../runtime/peer.md). A
runtime for another language needs this page, [the tunnel](tunnel.md), [the
session](session.md), [how a layer speaks](vocabulary.md) and [the driver
protocol](../../conformance/DRIVER.md), and nothing else.

## The connection beneath

A frames duplex connection: ordered, message-framed, bidirectional, closed
explicitly with a code and a reason, and nothing else — the seam. A
WebSocket is one; an in-memory pipe and a tunnel channel are others, and
every transport of a language is held to the seam's own suite. Close codes
are the WebSocket registry's numbers on every transport: 1000 normal, 1001
going away, 1002 protocol error, 1003 unsupported data, 1006 abnormal
closure, 1008 policy violation, 1009 too large, 1011 internal, and 4000–4999
for what runs above the seam; the profile itself closes with **4011** when
the other side broke it ([close codes are the WebSocket
registry's](../decisions/close-codes-are-the-websocket-registrys.md)).

The profile sends text frames only and refuses a binary frame; a frame
larger than the peer's limit is refused before delivery and the connection
with it. The seam frames every frame whole; the profile never splits one.
**1003** is what a frame of the wrong kind is refused with — the session
closes both of its connections with it, and with `a session speaks JSON text
frames` — where text that is no message of the profile is a fault of another
kind and carries another code.

## The subprotocol

A WebSocket handshake may negotiate a subprotocol, and the profile names
itself as none: a peer offers nothing by default, selects nothing by
default, refuses nothing on that ground, and reads nothing into what was
selected. `nightseam.duplex/1` is what an endpoint speaks, not a token on
the wire, and a peer that required its own name there would break every
deployment behind a server that selects none.

A consumer may nonetheless name something there, and two things want it: a
gateway or a proxy that tells a Nightseam socket from any other before
reading a frame — the profile's own name, or the family's service and
contract version, as every other WebSocket protocol family does it — and a
browser client's ticket, which has nowhere else to travel, a browser being
unable to set a header on an upgrade. The server reads the ticket off the
request as it reads everything else that authenticates a connection.

Offering and selecting are one decision, taken on both sides together: a
client that offers a subprotocol must be met by a server that selects one
of them, or the browser refuses the connection — which is why the defaults
are what they are ([no subprotocol by
default](../decisions/no-subprotocol-by-default.md)). A ticket is not a
list, so a server's selection may be a function of the request; how each
runtime exposes that is [the peer](../runtime/peer.md#the-subprotocol).

## The envelope

All JSON strings contain Unicode scalar values, including object member
names, envelope members and nested payloads. Malformed UTF-8 and unpaired
UTF-16 surrogate escapes are refused before decoding can replace them;
the check includes duplicate members a decoder would otherwise discard.
A valid surrogate pair represents its code point. An ordinary `�` (U+FFFD)
and ASCII text spelling `\\uD800` remain valid and unchanged. This is the
[admitted string domain](../decisions/strings-are-unicode-scalars.md).

One JSON object per frame, with `version` `1` and a `kind`:

| kind | members | rule |
| --- | --- | --- |
| `request` | `id`, `method`, `params` | `params` is present, `{}` where the method takes none |
| `response` | `id`, exactly one of `result` or `error` | `error` is `{code, message, data?}` with `code` and `message` non-empty |
| `event` | `event`, `data` | `data` is present, `null` where there is none |
| `cancel` | `id` | the request it withdraws |

Every kind may carry `traceparent` and `tracestate`, and a `request` and an
`event` may carry `meta` (§§ below). No other member is allowed; a member of
another kind, a duplicate member, an unknown member or trailing content after
the object makes the frame malformed. A malformed frame ends the connection
— a peer that sends one is not a peer to keep talking to — with 4011.

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
its own on the way out and maps the responses back ([the
session](session.md)).

## Requests

Either side may send a request at any time; requests are concurrent and a
response may arrive in any order. A request is answered exactly once, with a
`result` or an `error`. The runtime answers on the handler's behalf where
the handler does not:

| error code | when |
| --- | --- |
| `method_not_found` | no handler for `method` |
| `busy` | the receiver has as many requests open as it allows; or, locally and without a frame, the caller has as many calls outstanding as it allows |
| `cancelled` | the request was withdrawn, or its deadline passed, before the handler answered |
| `request_timeout` | the caller's own deadline passed; its own error, never a frame it received |
| `internal` | the handler failed with an error that is not public, or panicked; the message says nothing more |

A handler's own error crosses the wire only when it is a public one — a
value each runtime names for the purpose, carrying `code`, `message` and
optional `data` — and the family's declared errors are these, by name, in
the generated packages.

A **cancel** is best effort: it withdraws a request the receiver may already
have answered. The receiver cancels the handler's context; the handler's
response, if it still sends one, is `cancelled`. A cancel for an id that is
not open is ignored.

Every request has a deadline on the sender's side (30 seconds by default)
after which it is failed locally as a **deadline** — `request_timeout`, not
`cancelled` — and a cancel is sent: the caller withdrawing a request and
the caller giving up waiting for one are different facts ([a deadline is
not a cancel](../decisions/a-deadline-is-not-a-cancel.md)). An observer is
told the outcome `timeout`. The receiver's own deadline for a handler is
the same length, and what it answers on the wire when it passes is
`cancelled`, the request having been abandoned.

## Events

An event carries a name and data, expects no answer, and is delivered in
the order it was sent. Handlers run one at a time in that order; an event
with no handler is dropped. An event says nothing about receipt: sending
one completes when the frame was accepted for sending.

## Limits and backpressure

Every queue is bounded, per connection, and by default: 128 outgoing frames,
128 events waiting for their handlers, 128 calls outstanding at once, 64
requests being handled at once, frames of at most 1 MiB; a call's own
deadline is 30 seconds, a full queue's write deadline 10, and a dial's
handshake 30. Each bound is one option under one name in both languages,
the way the observer's events are one name in both — [the
peer](../runtime/peer.md#options-and-limits) tables them.

A producer that fills a queue is **paced for one write deadline** (10
seconds); a consumer that still has not drained it by then is disconnected
rather than allowed to hold the connection up. The rule is the same for
every queue and in both runtimes, because a burst that would drain in a
second should not end a connection ([queues are paced for one
deadline](../decisions/queues-are-paced-for-one-deadline.md)). Pacing an
*inbound* queue means pacing the remote, and the only way to do that is to
stop taking what it sends: while the event queue is full, the responses and
cancellations on that connection wait with the events. A runtime that
cannot pause what its transport hands it — a browser peer has no such knob
— holds the events instead of the reading; the deadline, and what happens
when it passes, are the same either way.

The two bounds on requests are different things and both are refusals rather
than failures: a **receiver** with as many requests being handled as it
allows answers `busy` on the wire, and a **caller** with as many calls
outstanding as it allows refuses the next one where it stands, without
sending a frame — no request is started, so no observer is told of one, and
the connection serves the call after it ([`busy` is a refusal, not a
failure](../decisions/busy-is-a-refusal-not-a-failure.md)).

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
replaces it — [the peer](../runtime/peer.md#trace-context-and-the-propagator)
says where — and the runtime imports none.

## Request metadata

A `request` and an `event` may carry `meta`, a flat object whose every value
is a string: what is about the call rather than the call — a tenant, an
idempotency key, a credential that is per request — carried outside the
family's declared types, so that a payload a consumer signs does not sign its
own carriage and a family generic in others can carry one for types it did
not declare. A `response` carries none, since what a server wants to say
about an answer is the result's business, and a `cancel` withdraws a call
rather than making one.

The form is the whole rule, and the profile reads no meaning into a key or a
value ([`meta` is a header, not a
member](../decisions/meta-is-a-header-not-a-member.md)). An empty object is a carriage like any other; `null`, a list, a string,
a number, and any value that is not a string make the frame malformed and end
the connection with **4011**, as any malformed frame does. Keys beginning
`nightseam.` are reserved for what the profile and its components may define
later — a deadline, a cause — and this version defines none, so a frame
carrying one is refused rather than read as a consumer's. `meta` counts toward
the peer's frame limit and nothing else bounds it.

What carries a frame carries `meta` with it: a relay forwards the member
verbatim, as it forwards a member it does not know, and an observer is told a
frame's name, id, size and trace and never a `meta` key or value — the rule
that no payload reaches an observer covers this carriage too.

A sender says what a frame carries and nothing carries it on by itself: a
handler's own calls carry none of what arrived unless the handler says so,
because a trace is the peer's to propagate and a credential is the consumer's
to pass on deliberately. A reserved key given to a sender is dropped rather
than sent. The two operations — say what the next frame takes, read what
arrived — are [the peer's](../runtime/peer.md#request-metadata).

## What the profile does not do

No retry, no reconnection, no acknowledgment of delivery, no
authentication: a connection is authenticated by whatever opened it — the
upgrade, before the profile begins — and the profile begins once it is open.
Resumption after a reconnect is a family's operation (`after` on a tunnel's
`channel.open`, a session's log), not the profile's.

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

## Observing it

A peer takes one observer and tells it ten things about the traffic it
carries — a connection opened and closed, a frame sent and received, a
request started and ended, an event emitted and delivered, backpressure, and
a handler that threw — each carrying names, ids, sizes, durations, outcomes
and the frame's trace, and none of them a payload. The tunnel and the session
running over the peer emit their own events through the same observer, so a
consumer chooses one. [The observer](../runtime/observer.md) has the rule and
every event of every layer.
