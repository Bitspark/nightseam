# The driver protocol

## Shared tables

The runtime unit suites and the conformance driver hold the same wire facts:

| table | holds |
|---|---|
| `tables/validator.json` | type expressions, values and their expected validation verdicts |
| `tables/frames.json` | envelopes a peer accepts or refuses |
| `tables/naming.json` | generated naming conventions |
| `tables/examples.json` | every example value, union arm and operation frame of the corpus documents, with its concrete parameter bindings or an explicit unavailable reason |

`examples.json` is derived by `go generate ./cmd/nightseam`. The generator's
test holds it byte for byte to the documents. Both runtime validator suites
read every concrete row with exactly its displayed type and family bindings;
frame rows also pass the profile's envelope decoder. Failures name the corpus,
family and document path. Unavailable rows carry no value, retain a reason,
and distinguish a bounded synthesis search (`limit`) from proven impossibility
(`impossible`); the current synthesizer claims only a search limit. The proof
family's examples are required coverage, not optional rows.

## Driving a language

A language proves it carries Nightseam by passing the conformance suite: the
scenarios under `scenarios/`, run by the Go runner under `go/` against a
**testee** the language provides — one small program that is a peer under
remote control. The runner never speaks the profile itself; it starts two
testees, tells each what to do, and holds what happened to the scenario. Go's
testee is the reference: every scenario passes with Go on both sides before
it is asked of anyone else, and a language is held to Go on both sides of
the wire in turn.

This page is the protocol between the runner and a testee. It is versioned
by the `driver` number the testee answers `hello` with; this is driver 1.

## Transport

The runner starts the testee as a child process and speaks JSON lines over
its standard streams: one request per line on stdin, one answer per line on
stdout. Nothing but answers is written to stdout — a testee that logs, logs
to stderr, which the runner keeps and shows with a failure. Stdout is
flushed after every line. The runner sends one request at a time and waits
for its answer before sending the next; a testee never writes unasked.

```
→ {"id": 7, "op": "peer.dial", "url": "ws://127.0.0.1:41263", "observe": true}
← {"id": 7, "ok": {"handle": "p2"}}
→ {"id": 8, "op": "call.await", "on": "c1", "within_ms": 2000}
← {"id": 8, "error": {"code": "timeout", "message": "no response within 2000ms"}}
```

A request is an object with `id` (an integer the answer repeats), `op`, and
the op's arguments as further members — so no op names an argument `id` or
`op`. An answer is `{"id", "ok": …}` with
the op's result, or `{"id", "error": {"code", "message"}}`. The codes the
protocol itself defines:

| code | meaning |
|---|---|
| `unsupported` | the testee does not implement the op, or the feature the arguments ask for; the runner **skips** the scenario and says why |
| `timeout` | the op did not settle within its `within_ms` |
| `unknown_handle` | `on` names nothing this testee minted |
| `invalid` | the arguments are malformed; a testee bug or a runner bug |
| `cancelled`, `request_timeout`, `disconnected` | how a call ended, see *Calls* |
| any other | a public error the remote answered with, its code verbatim |

The first request is always `hello`, and the testee answers with what it is:

```
→ {"id": 1, "op": "hello"}
← {"id": 1, "ok": {"driver": 1, "language": "go", "layers": ["seam", "peer", "tunnel"],
                   "features": ["listen", "pipe", "observer", "propagator", "lazy"]}}
```

`layers` names the op families the testee implements; a scenario names the
layers and features it `needs`, and the runner skips it for a testee that
lacks one. The features:

| feature | means |
|---|---|
| `listen` | the testee can accept a connection (`conn.listen`, `peer.listen`); a language whose runtime only dials lacks it and still runs both roles, since the runner has the other side listen and takes the peer over the connection with `peer.over` — see *Who listens* |
| `pipe` | `conn.pipe`, an in-process connected pair |
| `observer` | `observe: true` on a peer, and `peer.observed` |
| `propagator` | `propagate: true` on a peer: traces are minted and carried |
| `lazy` | `consume: "lazy"` on a connection |

Between scenarios the runner sends `reset`: the testee closes and forgets
everything it holds — every connection, peer, listener, tunnel — and
answers `{}` with nothing left running, so that one process serves a whole
run and no scenario sees another's state. The last request is `bye`; the
testee resets and exits 0. A testee that exits before `bye`, or writes a
line that is not an answer, fails the scenario it was in and is started
again for the next.

## Handles

Everything a testee makes is a **handle**, a string it mints and the runner
passes back as `on`: a connection, a listener, a peer, a tunnel, a channel,
a call. A channel is also a connection, and takes
every `conn.*` op. Handles are never reused within a process.

## Values

Payloads travel as JSON, byte for byte. Where an argument is a payload —
`params`, `data`, `result`, `value`, a frame's `text` — the runner sends the
scenario's bytes unchanged and the testee hands them to its runtime
unchanged; a testee does not decode and re-encode a payload on its way
through, since `1e3` and `1000` are the same number and not the same bytes,
and what a decoder is held to is the bytes. A result the testee reports back
is whatever its runtime decoded, re-encoded; the runner compares results as
JSON values, not bytes.

Binary is base64 in a `base64` member. Durations are `_ms` integers. Times
are never reported: every event a testee reports has its `at` stripped.

## Waiting

An op that may wait on the other side of the wire takes `within_ms` and
answers `{"error": {"code": "timeout"}}` when it passes, never hangs: the
runner is blocked on the answer, and a testee that waits forever is a
scenario that never ends. The default is 5000. Every `await_*` op takes it;
so do `conn.send`, `conn.close`, `peer.emit`, `peer.accept`, `conn.accept`,
`tunnel.open` and `tunnel.accept`, each of which can block on the peer
process.

The testee is **pull-based**. It keeps, per handle, what arrived while the
runner was not asking: frames received on a connection, events received on
a peer, the lifecycle of every request a canned handler served, and what an
observer was told. An `await_*` op returns the
first entry that matches, removing it; a `drain` op returns everything held
and empties it. Nothing is reported unasked and nothing is lost between
asks. An op that reads what is held without waiting — `peer.observed`,
`drain` — reads it as it is at that moment, so a scenario expecting there
what the other side has yet to send, such as the cancel a caller writes
*after* its own call has failed on its deadline, says `"repeat": {"max":
…, "until": "match"}` on the step, and the runner asks again until the
expectations hold; a scenario that reads once is a scenario that fails when
the wire is slow.

An observer snapshot repeated until a match must set `"drain": false`.
The loader refuses one that does not: otherwise the first poll can consume
the request events before its cancel is observed, leaving no later answer
with all the evidence. A one-shot observer read still drains by default.

## Inbound consumption

A connection made with `"consume": "lazy"` receives nothing until
`conn.receive` asks; one made `"eager"` (the default) receives as frames
arrive and holds them. Lazy is what a scenario about credit needs: a channel
whose reader has stopped is the only way a sender stalls. A testee that
lacks `lazy` says so in `hello` and the runner skips those scenarios.

## Who listens

A scenario never says which side opens the socket. It opens its connections
with the runner's own ops, written `"on": "runner"`, and the runner expands
each into the testees' ops from what the two answered `hello` with:
whichever side can listen does, so that a language whose runtime only dials
is held in every role all the same. The profile's role — server, client —
is the scenario's to assign; who listens beneath it is the runner's to
choose, and a peer of either role is taken over a connection either side
accepted, through `peer.over`.

| op | arguments | binds |
|---|---|---|
| `pair.conns` | `limit_a`, `limit_b`, `consume_a`, `consume_b` — each side's own receive limit and consumption | `{"a", "b"}` the two connections |
| `pair.peers` | **`server`** `"a"`\|`"b"`, `server_options`, `client_options` | `{"server", "client"}` the two peers |
| `pair.peer_and_conn` | **`peer`** `"a"`\|`"b"`, **`role`**, `options` (the peer's), `limit`, `consume` (the raw side's) | `{"peer", "conn"}` |

The paths, in the order the runner tries them: the natural side listens
(`peer.listen`, `peer.dial`, `peer.accept`, or the seam's three); else the
other side listens a raw connection and the peers are taken over its ends
with `peer.over`, lazily consumed since a peer reads its own connection;
else — neither can listen — the scenario is skipped for that pairing with
that reason. A scenario that is *about* listening, a WebSocket handshake
say, writes `conn.listen` or `peer.listen` itself and needs `listen` on
that side.

What a scenario `needs` is held **per side**: from each side's steps the
runner derives the layer of every op and the features the ops and their
arguments use — `listen`, `pipe`, `lazy`, `observer` for `peer.observed`,
`propagator` for `propagate: true` — and holds that side's testee to them
alone, so a language that lacks a feature skips a scenario only where it
would use it. The file's `needs` is the union over both sides, which the
runner refuses a file to differ from: it is what places the scenario in a
profile, and documentation that cannot drift.

## Ops

Arguments in **bold** are required. Every op takes `on` where it acts on a
handle. An op on a handle of the wrong kind answers `invalid`.

### Seam — `conn.*`

The frames duplex connection beneath the profile: `duplex/go`'s `Conn`,
`@nightseam/duplex`'s `FrameConnection`.

| op | arguments | answer |
|---|---|---|
| `conn.listen` | `limit` (bytes, default 1 MiB) | `{"handle", "url"}` — a listener accepting one WebSocket at `url` |
| `conn.accept` | **`on`** listener, `consume`, `within_ms` | `{"handle"}` the accepted connection |
| `conn.dial` | **`url`**, `limit`, `consume` | `{"handle"}` |
| `conn.pipe` | `limit`, `consume` | `{"a", "b"}` two handles, connected in-process |
| `conn.send` | **`on`**, **`kind`** `"text"`\|`"binary"`, `text` or `base64`, `within_ms` | `{}` |
| `conn.receive` | **`on`**, `within_ms` | `{"kind", "text"}` or `{"kind", "base64"}`; a close is `{"error": {"code": "closed", "close_code", "reason"}}` |
| `conn.close` | **`on`**, `code`, `reason`, `within_ms` | `{}` |
| `conn.abort` | **`on`** | `{}` |
| `conn.await_close` | **`on`**, `within_ms` | `{"code", "reason"}` as the connection ended: a close frame's, or `1006` and `""` for an abort or a broken transport |

### Peer — `peer.*`, `call.*`

The profile: `runtime/go`'s `Peer`, `@nightseam/runtime`'s `DuplexPeer`.

| op | arguments | answer |
|---|---|---|
| `peer.listen` | `options`, `subprotocols` | `{"handle", "url"}` — a listener accepting one peer at `url`, as the server |
| `peer.accept` | **`on`** listener, `within_ms` | `{"handle", "subprotocol"}` the accepted peer, with the listener's `options` |
| `peer.dial` | **`url`**, `options`, `subprotocols` | `{"handle", "subprotocol"}` the client peer, connected |
| `peer.over` | **`on`** connection or channel, **`role`** `"client"`\|`"server"`, `options` | `{"handle"}` a peer speaking the profile over that connection |
| `peer.handle` | **`on`**, **`method`**, **`behavior`** | `{}` — registers a canned handler, see below |
| `peer.on_event` | **`on`**, **`name`**, `behavior` | `{}` — how an event is taken: `record` (default), `block`, `panic` |
| `peer.call` | **`on`**, **`method`**, `params`, `timeout_ms`, `meta` | `{"handle"}` a call, in flight |
| `call.await` | **`on`**, `within_ms` | `{"result": …}` or `{"error": {"code", "message", "data"}}` |
| `call.cancel` | **`on`** | `{}` — the caller gives up; its `call.await` then ends `cancelled` |
| `peer.emit` | **`on`**, **`event`**, `data`, `within_ms`, `meta` | `{}` |
| `peer.await_event` | **`on`**, **`name`**, `within_ms` | `{"data": …, "meta"?}` |
| `peer.await_request` | **`on`**, **`method`**, **`phase`** `"started"`\|`"ended"`, `within_ms` | `{"id", "method", "phase", "outcome", "meta"?}` — what a canned handler saw; `outcome` on `ended` is `ok`, `error`, `cancelled` or `panic` |
| `peer.observed` | **`on`**, `trace` (bool), `drain` (bool, default true) | `[event, …]` what the observer was told, normalized, see below |
| `peer.close` | **`on`** | `{}` |
| `peer.await_close` | **`on`**, `within_ms` | `{"clean": bool, "code": int}` — the code the connection ended under and whether it was a close somebody chose |

`meta` on `peer.call` and `peer.emit` is the profile's carriage the frame
takes: an object whose every value is a string, absent by default. A testee
sends it the way its language says what a frame carries — a context the call
is made under, an option beside the params — and never adds one of its own,
so a step that names none produces a frame with the member absent rather
than empty. `peer.await_request` on the `started` phase and `peer.await_event`
report what the frame they answer for carried, under `meta`, and leave the
member out where it carried none; that is how a scenario holds that a
carriage reached the handler it was addressed to and went no further. A key
of the reserved `nightseam.` prefix is dropped before sending rather than
refused, since the peer at the far end refuses a frame carrying one.

`subprotocols` is an array of tokens, empty or absent by default. On
`peer.listen` it is what the server will select from, in its own order of
preference; on `peer.dial` it is what the client offers, in its own. The
`subprotocol` both answers carry is what the handshake selected as that side
sees it, `""` when it selected none — which a connection over anything but a
WebSocket always is. A testee whose transport cannot negotiate one answers
`unsupported` and the scenario is skipped for it.

`peer.await_close` reports the code the peer's own observer was told the
connection ended under, which is the code the wire carried: **1000** where a
side chose the close, **4011** where a peer refused a frame of the profile,
**1006** where a side aborted and sent nothing at all, and whatever the remote
sent where the remote closed first. `clean` is that code being 1000 — a close
somebody chose — and a peer that ended on a transport failure or on a refusal
is not clean. The seam's `conn.await_close` reads the same fact off the
connection rather than off the peer.

`options` on `peer.listen`, `peer.dial` and `peer.over`:

| member | default | means |
|---|---|---|
| `max_frame_bytes` | the runtime's | a frame over it is refused and the connection ended |
| `queue_capacity` | the runtime's | inbound events held before the peer is stalled |
| `max_pending_requests` | the runtime's | calls this peer may have outstanding at once; the one past it is refused `busy` without reaching the wire |
| `request_timeout_ms` | the runtime's | a call's deadline |
| `write_timeout_ms` | the runtime's | how long a send may wait, and how long a full queue is paced before its consumer is stalled |
| `families` | `{}` | method or event name → family, what the observer's `family` says |
| `observe` | `false` | keep what an observer is told, for `peer.observed` |
| `propagate` | `false` | the runtime's default propagator is in use; every request carries a trace and a handler's callbacks parent on it |

**Calls.** `call.await` answers as the call ended: the remote's result; the
remote's public error with its `code`, `message` and `data` verbatim; or one
of the driver's own — `cancelled` after `call.cancel`, `request_timeout`
when the peer's deadline passed, `disconnected` when the connection ended
first. A testee maps its runtime's errors onto these three; their messages
are its own.

**Canned behaviours.** A handler the runner installs does one of a fixed set
of things, so that a testee is a peer under control and not a script engine:

| `behavior` | does |
|---|---|
| `{"kind": "echo"}` | answers with its params |
| `{"kind": "return", "value": …}` | answers with `value` |
| `{"kind": "fail", "code", "message", "data"}` | answers with that public error |
| `{"kind": "wait"}` | answers nothing until the request is cancelled or the peer ends; `peer.await_request` sees `started`, then `ended` with `cancelled` |
| `{"kind": "hold", "until": "<event>", "value": …}` | holds the request until the remote emits `until`, its signal notwithstanding, and then answers `value` — the one handler that does not stop when it is told to, which is how a scenario holds *when* a withdrawn request is answered |
| `{"kind": "panic", "value": "…"}` | panics, throws — whatever the language does when a handler gives up — with that value |
| `{"kind": "reverse", "method", "params"}` | calls `method` on the remote **from the request's own context**, so the call is a child of the request's trace, with `params` or, absent, its own; answers with what came back, or fails with what came back |
| `{"kind": "emit", "event", "data", "then": …}` | emits `event` from the request's context, then answers `then` |
| `{"kind": "through", "attachment"}` | only for a live binding: invokes the imported binding `attachment` names and answers with what came back — which is how a callable that was *returned* reaches a callable that was *supplied* |

Every canned handler records its lifecycle for `peer.await_request`: `started`
when invoked, `ended` when it answered, with the outcome. An event handler
does `record` (the event is held for `peer.await_event`), `block` (the
handler never returns, which is how an inbound queue fills), or `panic`.

**What an observer is told**, as `peer.observed` reports it. One shape in
every language, lower snake case, `at` stripped:

| event | members |
|---|---|
| `connection.opened` | `role` |
| `connection.closed` | `code`, `reason`, `local` |
| `frame.sent`, `frame.received` | `kind`, `name`, `bytes`, `id`, `family` |
| `request.started` | `id`, `method`, `incoming`, `family` |
| `request.ended` | `id`, `method`, `incoming`, `duration`, `outcome` (`ok`\|`error`\|`cancelled`\|`timeout`), `error_code`, `family` |
| `event.emitted`, `event.delivered` | `name`, `bytes`, `family` |
| `backpressure` | `queued`, `stalled`, `deadline` |
| `handler.panic` | `method`, `value`, `family` |
| a tunnel's | as its layer says, below |

`bytes` is reported as `true` when positive, `duration` and `deadline` as
`true` when non-negative: a scenario holds that a size or a time was
measured, not what it was. An absent or empty string member is omitted, so
`id` is absent on an event frame and `family` on an unlabelled name. With
`"trace": true` an event that concerns a frame carries
`"trace": {"trace_id", "span_id", "flags", "state"}` — the traceparent split,
the tracestate verbatim, `state` omitted when empty — and one that concerns
none, or whose frame carried none, carries no `trace` member. A panic's
`value` is the language's rendering of what was thrown; a scenario matches
it with a pattern.

### Tunnel — `tunnel.*`

Channels multiplexed over one peer: `tunnel/go`, `@nightseam/tunnel`.

| op | arguments | answer |
|---|---|---|
| `tunnel.over` | **`on`** peer, `options` (`window`, `max_frame_bytes`, `accept_capacity`, `contracts`: family-to-digest map) | `{"handle"}` |
| `tunnel.open` | **`on`**, **`family`**, `digest`, `consume`, `within_ms` | `{"handle", "id"}` — the channel, a connection handle |
| `tunnel.accept` | **`on`**, `consume`, `within_ms` | `{"handle", "id", "family", "digest"}` — empty digest means unspecified |

A channel takes every `conn.*` op, and `peer.over` makes a peer of it. A
tunnel observes through its peer's observer; its events reach
`peer.observed` on that peer: `channel.opened` (`family`, `id`,
`opener`), `channel.accepted` (`family`, `id`), `channel.closed`
(`family`, `id`, `code`, `reason`), `credit.stall` (`family`, `id`,
`waiting`), `open.refused` (`family`, `reason`).

A scenario provokes a credit violation with `peer.emit` on the outer peer
rather than with `conn.send` on the channel: a channel's own send waits for
credit it was not granted, so the only way to put a frame beyond the window on
the wire is to write a `channel.frame` event on the peer the tunnel runs over,
naming the channel by the `id` `tunnel.open` answered with. That is what a peer
of another making may do, and what the receiving side is held to refusing.

### Live — `live.*`

Callable values across one connection: `live/go`, `@nightseam/live`. A binding
is one function made addressable from the other side; the layer speaks
`live.invoke` and `live.release` as ordinary frames of the profile, so a live
scope is made over a **peer** and needs no tunnel.

| op | arguments | answer |
|---|---|---|
| `live.over` | **`on`** peer, `options` (`max_exports`, `max_imports`) | `{"handle"}` |
| `live.owner` | **`on`** scope, `owner` parent handle, `root` | `{"handle"}` — a child of the parent (the root by default), or the root itself when `root: true` |
| `live.owner_release` | **`on`** owner | `{}` — release this owner and its descendants, idempotently |
| `live.owner_counts` | **`on`** owner | `{"exports", "imports"}` — bindings owned here, excluding borrows |
| `live.import_value` | **`on`** scope, `owner`, **`references`**, **`contract`**, `fail` | `{"handles"}` — imports in one batch; `fail: true` refuses after the imports to exercise rollback |
| `live.export` | **`on`** scope, **`contract`**, `behavior` | `{"reference"}` — the reference as it travels in a payload |
| `live.import` | **`on`** scope, **`reference`**, **`contract`** | `{"handle"}` an attachment |
| `live.invoke` | **`on`** attachment, `request`, `timeout_ms`, `cancelled` | `{"handle"}` a call, in flight |
| `live.release` | **`on`** scope, **`reference`** | `{}` |
| `live.forward` | **`on`** the destination scope, **`contract`**, **`attachment`** | `{"reference"}` |
| `live.counts` | **`on`** scope | `{"exports", "imports"}` |
| `live.await_invocation` | **`on`** scope, `contract`, `within_ms` | `{"contract", "outcome"}` — what an exported binding was asked |
| `live.close` | **`on`** scope | `{}` |

An invocation **is a call**: `live.invoke` answers with a call handle, and
`call.await` and `call.cancel` act on it as they do on `peer.call`'s. That is
not a convenience of the driver — it is the layer's contract, and it is how a
scenario holds that withdrawing an invocation and releasing a binding are two
different things.

`live.invoke` with `cancelled: true` cancels its context or signal before
calling the imported function. It holds pre-existing cancellation separately
from `call.cancel`, which withdraws an invocation after it has started.

A `reference` is the value a `live.export` answered with, passed along by the
scenario; a testee decodes it through the scope it is importing into, since a
reference of no scope is not a reference. A scenario may also write one by hand
to name a binding nobody exported, which is the stale token.

`behavior` is the same canned set a peer's handler takes, plus `through`. A
binding that `wait`s never settles of its own accord, and one that `hold`s waits
for the remote to emit its `until` event however it is cancelled.
Its `started` observation is a barrier: the event listener is installed before
that observation is made. The exporter and local-call closure scenarios await
caller settlement before sending the event, then verify ordinary RPC still works.

`live.counts` is the suite's leak assertion: every `live.*` scenario ends by
naming what each scope still holds, so a retained binding fails a scenario whose
payloads all matched. A refusal that left something half-registered is visible
there and nowhere else.

`live.export`, `live.import`, and `live.forward` take an optional `owner`
handle belonging to their scope. Without one, acquisition uses the root
owner. `live.release` still releases a reference binding-wide. The owner
scenario releases only owners and keeps both peers open; an owner that
borrowed an existing attachment owns no import and cannot revoke it.

The refusals are the layer's own codes, answered under *any other* above:
`contract_invalid`, `contract_mismatch`, `reference_unknown`,
`reference_foreign`, `reference_released`, `scope_closed`, `too_many_exports`,
`too_many_imports`. A scope observes through the peer it runs over; its events
reach `peer.observed` there: `live.exported` (`contract`, `binding`),
`live.imported`, `live.released` and `live.refused` (`contract`, `code`,
`reason`).

### Generated code — `gen.*`, `client.*`

A language's second testee links the packages the generator renders for the
corpus's `probe` family over that language's runtime: Go and TypeScript
each link their client and server binding. The runner renders the
families and builds this testee from the recipe
in `testee.json` before the `generated` scenarios run.

| op | arguments | answer |
|---|---|---|
| `gen.serve` | `behaviors` | `{"handle", "url"}` — the binding served at `url` with the canned handler set below |
| `gen.dial` | **`url`**, `options` | `{"handle"}` a generated client, its reverse-call handler canned |
| `client.echo` | **`on`**, **`params`** | `{"result"}` or `{"error"}` as the typed call ended |
| `client.no_args` | **`on`** | the same |
| `client.seen` | **`on`**, **`params`** | the same |
| `client.emit_noticed` | **`on`**, **`data`** | `{}` |
| `client.await_changed` | **`on`**, `within_ms` | `{"data"}` |
| `client.await_notification` | **`on`**, `within_ms` | `{"event", "data"}` — the next typed callback, in delivery order |
| `client.close` | **`on`** | `{}` |
| `server.reverse` | **`on`** the served handle, **`params`** | `{"result"}` or `{"error"}` — the binding calls the connected client's `reverse` |
| `server.emit_changed` | **`on`**, **`data`** | `{}` |
| `server.await_noticed` | **`on`**, `within_ms` | `{"data"}` |
| `gen.validate` | **`type`**, **`value`** | `{"valid": true}` or `{"valid": false, "message"}` — the protocol package's validator on a type expression |
| `gen.errors` | | `["code", …]` the family's public errors, sorted |
| `gen.is_error` | **`code`** | `{"value": bool}` — whether the code is one the family declares, which the rendering names |

The canned server: `echo` answers the payload with `text` reversed;
`no_args` answers `"none"`; `seen` answers `[]`; `reverse` on a client
answers the payload with `text` prefixed by the language's name and a colon,
`"go:"`, `"typescript:"`; a `changed` event is held for `client.await_changed`
and a `noticed` event for `server.await_noticed`. A scenario does not know
which language is on each side, so it holds the prefix with a pattern.

The Go and TypeScript testees implement every generated server operation
through their generated binding packages. TypeScript's testee accepts the
WebSocket and passes it to generated `serve`; the host helper supplies no
protocol dispatch or conversion. `mirror: true` exchanges driver sides `a`
and `b`, so the cross-language suite exercises each language serving and
calling. The [role inventory](../docs/declaration/proof-findings.md#generated-roles-and-skips)
maps the operations to scenarios. A language without a required generated
role reports `unsupported`; the [tier policy](../docs/languages/tiers.md)
defines what that skip means for its verdict.

The generated testee lies under `conformance/<lang>/generated/`, and the
runner lays those files beside the probe rendering in `{rendered}` before
the recipe's `generated.build` runs: a `.tmpl` suffix is dropped,
`{checkout}` and `{go}` (the checkout's go directive) are filled in every
file, and a file named `go.sum.checkout` is replaced by the checkout's
`go.sum`. The rendering is rooted at module `example.test/generated` and
scope `@example`.

### The type-language proof

The runner also renders the shared corpus's `proof` family beside `probe`.
Its generated handlers live in separate testee files. The TypeScript recipe
compiles the testee and generated packages before running them.

| op | arguments | result |
|---|---|---|
| `gen.proof_serve` | | `{"handle", "url"}` — the generated proof binding, with probe family and string item bindings |
| `gen.proof_dial` | **`url`** | `{"handle"}` — a generated proof client bound to the probe family and string item type |
| `client.proof_call` | **`on`**, **`method`**, **`params`**, `raw`, `within_ms` | `{"result"}` or `{"error"}`; methods are `classify`, `classify_rich`, `parts`, and `relay`; `raw: true` bypasses the client codec to exercise the binding's refusal |
| `server.proof_emit` | **`on`**, **`data`**, `within_ms` | `{}` — emits the generated `part.added` event |
| `client.proof_event` | **`on`**, `within_ms` | `{"data", "kind"}` — a generated callback's value, dispatched by its native discriminator |
| `gen.proof_names` | | the sorted inline type names referenced by the compiled testee |
| `gen.proof_validate` | **`type`**, **`value`** or **`text`** | `{"valid": true}` or `{"valid": false, "code": "invalid", "message"}` |

`type` is a type expression encoded as a JSON string, as in `gen.validate`.
`text` supplies original JSON source for a malformed-Unicode case, which
must be checked before decoding; it replaces `value`. The `invalid` code
is the driver's argument-refusal code, not a new exported validator error.
The canned proof binding dispatches unions to a kind-prefixed string,
returns a page of three parts, and relays a typed envelope in `Option`.
Its inherited echo returns the payload unchanged. Mirrored wire scenarios
attempt both roles in both ordered language pairings, using each
language's generated binding and client.

#### The live tier — `gen.live_*`, `client.live_*`

The same testee also links what the generator renders for the corpus's
`worker` family, whose live tier declares callables. These ops drive it, and
nothing in them touches the live runtime: every callable a testee hands over
or receives is an ordinary function of its language, which is what the tier
promises and what these scenarios are for.

| op | arguments | answer |
|---|---|---|
| `gen.live_serve` | | `{"handle", "url"}` — the worker binding, served; unsupported in a target without bindings |
| `gen.live_dial` | **`url`** | `{"handle"}` a generated worker client, its reverse-call handler canned |
| `client.live_describe` | **`on`**, **`ticket`**, **`label`** | `{"label"}` — ordinary RPC over a family that has a live tier, carrying no callable |
| `client.live_start` | **`on`**, **`ticket`**, **`label`** | `{"job", "ticket"}` — the request carries a sink this testee implemented; the answer names the returned record of callables |
| `client.live_reports` | **`on`** | `{"values"}` — what this testee's own sink was told, in order |
| `client.live_cancel` | **`on`**, **`job`** | `{}` — the returned `cancel`, called |
| `client.live_rename` | **`on`**, **`job`**, **`ticket`**, **`label`** | `{"label"}` — the returned `rename`, called, with a request and a result of its own |
| `gen.live_report_again` | **`on`** | `{}` — the served side calls the sink it kept, after the call that supplied it returned |
| `gen.live_supervise` | **`on`**, **`sinks`**, `within_ms` | `{"state"}` — waits for the server's client attachment, then calls the client with a map of callables and reads back a sum, within one deadline |
| `gen.live_seen` | **`on`** | `{"started", "calls"}` — how many starts the served side took, and what was called on it |

#### Higher-order callables and generic containers — `gen.combinator_*`, `client.combinator_*`

The same testee links the corpus's `combinator` family, whose callables take
or answer callables, and the `boxes` family it applies to them. Two
scenarios drive it: `generated/live-higher-order-callables.json` and
`generated/live-generic-containers.json`. A toolkit is what
`client.combinator_toolkit` answered — this testee's own name for the record
it received, as `job` is above.

| op | arguments | answer |
|---|---|---|
| `gen.combinator_serve` | | `{"handle", "url"}` — the combinator binding, served; unsupported in a target without bindings |
| `gen.combinator_dial` | **`url`** | `{"handle"}` a generated combinator client, its reverse-call handler canned |
| `client.combinator_toolkit` | **`on`**, **`seed`** | `{"toolkit"}` — an ordinary call answering a record whose three members each take or answer a callable; the served side keeps the seed for `apply` |
| `client.combinator_twice` | **`on`**, **`toolkit`**, **`add`**, **`with`** | `{"value"}` — this testee's `x + add` handed to the other's `twice`, which answers a callable this testee then invokes with `with` |
| `client.combinator_identity` | **`on`**, **`toolkit`**, **`with`** | `{"value"}` — a callable answered by a callable that takes nothing, invoked with `with` |
| `client.combinator_apply` | **`on`**, **`toolkit`**, **`factor`** | `{}` — this testee's `x * factor` handed to a callable that answers nothing; the served side calls it with the seed |
| `gen.combinator_seen` | **`on`** | `{"applied"}` — what the served side's `apply` was answered, in order |
| `client.combinator_pack` | **`on`**, **`add`**, **`with`**, `within_ms` | `{"value", "seed", "none", "null", "absent", "extra"}` — see below |

`client.combinator_pack` supplies a callable in `boxes.Box`, receives it
through `boxes.Batch` (a generic alias, open record, inherited union, map and nullable), and
invokes a returned generic live record's function after the RPC. That record
captures its parameter in an inline metadata record. It answers `{"value", "seed", "none",
"null", "absent", "extra"}`, holding the callback result, preserved empty
forms and an additional field on the open record.

An argument is merged into the request envelope, so no op names one `id`;
`ticket` is the entity key where one is meant. `job` names what
`client.live_start` answered, which is this testee's own name for the record
it received — a reference is never a value a scenario writes.

#### Explicit owners — `gen.owners_*`, `client.owners_*`

`generated/live-owners.json` renders the test-only `owners` family alongside
the corpus. Its ordinary generated `create` takes `worker.Start` and returns
`boxes.Page<worker.Job>`; `pack` takes `boxes.Box<combinator.Unary>` and returns
`combinator.Bundle<Count>`. The caller supplies an owner, and the handler
keeps the child supplied through its context. `drop` is an ordinary generated
RPC whose implementation later releases those saved handler owners.

| op | arguments | answer |
|---|---|---|
| `gen.owners_serve` | | `{"handle", "url"}` — a generated binding configured for four exports and four imports |
| `gen.owners_dial` | **`url`**, `max_imports` (default 4) | `{"handle"}` — generated client, four exports and the chosen import bound |
| `client.owners_create` | **`on`** | `{"label", "reports", "counts"}` — sends a native callback, retains a returned `Page<Job>`, then calls its native cancel and rename functions |
| `client.owners_pack` | **`on`** | `{"value", "seed", "counts"}` — calls the generic returned function, which calls the supplied callback twice |
| `client.owners_release` | **`on`** | `{"exports":0,"imports":0}` — releases the caller owner twice, asks the handler to release its owners and awaits baseline |
| `client.owners_revoke` | **`on`** | `{"error", "counts"}` — the handler releases its owner first; the next retained function call is refused |
| `client.owners_imports` | **`on`** | `{"fresh", "borrowed", "repeated", "counts"}` — a bound-two client attempts partial record and alias-prefixed conversions, keeps a prior attachment callable, and proves repeated references borrow one attachment |
| `gen.owners_counts` | **`on`**, `within_ms` | `{"exports":0,"imports":0}` once the server reaches baseline |

The import-refusal operation uses a fixture-only `references` method returning
three valid generated `Report` exports. Importing them exceeds the client's
capacity and exercises rollback of a partially completed conversion;
it is separate evidence from the ordinary generated
operations above. No owner operation extracts a reference from a native
value to release it, and no connection closes to make a count assertion pass.

#### Uncertain publication — `gen.publication_*`

`generated/live-uncertain-publication.json` renders the test-only
`publication` family from `conformance/corpora/live-publication`. Its
generated client and server binding both send native callbacks through
methods and events. Each connection permits four exports and four imports,
and a frame is bounded to 1,024 bytes.

| op | arguments | answer |
|---|---|---|
| `gen.publication_serve` | | `{"handle", "url"}` — the generated binding with handlers that retain their generated child owners |
| `gen.publication_dial` | **`url`** | `{"handle"}` — the generated client with the same reverse-call behavior |
| `gen.publication_exercise` | **`on`** either handle, `within_ms` | `{"completed", "cycles":16, "counts":{"exports":0,"imports":0}}` — all publication cases on the handle's outbound generated API |

The sixteen cases are seven fresh callback supplies refused with `busy`,
then remote `cancelled` and `frame_too_large` refusals, local timeout,
cancellation, a suppressed successful reply, a callback event, a returned
callback whose reply is suppressed, a pre-cancelled request, and an oversized
event. A received callback invocation is the delivery barrier: cancellation
occurs only after it, and timeout cases must reach it before they settle.
For the returned callback, the handler waits for cancellation before
returning its native function; its saved child owner must then hold the
export even though the result cannot reach the caller.

Every uncertain case calls the retained callback after the supplying
operation ends, releases the caller's owner, checks the remote alias refuses
with `reference_released`, and releases the handler's saved owner. The two
local refusals must carry `UnpublishedError` and leave no new export; the
other outcomes must carry no such proof. Each case waits for both scopes to
reach zero before the next begins. Ordinary generated inspection and drop
calls prove the connection remains usable throughout. The scenario exercises
both handles and mirrors the driver sides, holding both languages in every
generated role without a raw-reference release or reconnection.

#### Higher-order forwarding — `gen.forwarding_*`

`generated/live-higher-order-forwarding.json` uses three logical peers on
two real WebSockets. One testee owns endpoints A and C at different URLs;
the other owns B's two clients/scopes. Mirroring exchanges which testee
owns the endpoints. Both Go and TypeScript supply generated endpoint
bindings and perform every intermediary operation with their generated
callable converters.

A's generated `combinator` binding answers a `Toolkit`. At B, generated
`ImportToolkit`/`importToolkit` reads it in A-B's scope, and
`ExportToolkit`/`exportToolkit` wraps the native functions for B-C. C's
test-only `fixture.forwarding.retain` route validates the descriptor and
calls the generated import helper. This route is driver plumbing alongside
the generated binding, not a new declared operation.

| op | arguments | answer |
|---|---|---|
| `gen.forwarding_serve` | | `{"handle", "url"}` — a generated endpoint binding with a test-only retain route |
| `gen.forwarding_dial` | **`origin`**, **`destination`**, `within_ms` | `{"handle", "different_nonces"}` — B imports from A and exports into C, checking distinct binding nonces |
| `gen.forwarding_capture` | **`on`**, `within_ms` | `{}` — C calls the retained factory, producer and sink, keeping the returned functions |
| `gen.forwarding_invoke` | **`on`**, **`with`**, **`identity_with`**, `within_ms` | `{"twice", "identity"}` — C invokes those retained results after their supplying calls ended |
| `gen.forwarding_applied` | **`on`** | `{"values"}` — what A's sink received from C's callback |
| `gen.forwarding_release_destination` | **`on`** | `{}` — B releases its destination factory binding, leaving the origin untouched |
| `gen.forwarding_origin_alive` | **`on`**, `within_ms` | `{"value"}` — B invokes the original factory with `x + 10`, then its result with `1` |
| `gen.forwarding_release_origin` | **`on`** | `{}` — B releases its imported origin producer, whose destination wrapper remains exported |
| `gen.forwarding_origin_refusal` | **`on`**, `within_ms` | `{"error"}` — C invokes that producer and observes the origin's refusal |
| `gen.forwarding_await_counts` | **`on`**, **`counts`**, `within_ms` | the actual counts once they match, or `timeout` — an endpoint has `{"exports", "imports"}`; B has `{"origin": {…}, "destination": {…}}` |

All counts are checked while the peers remain open. Previously returned
functions have separate bindings and remain callable after parent release;
their retained exports/imports stay visible in the counts. `reset` closes
both sockets and every scope. This scenario exercises recursive generated
conversion at declared callable positions; raw `Forward`/`forward` only
re-exports an `Invoke` and does not translate embedded references.

## `testee.json`

A language joins the suite with `conformance/<lang>/testee.json`:

```json
{
  "language": "typescript",
  "toolchains": ["node"],
  "build": [],
  "run": {"argv": ["node", "--experimental-strip-types", "--no-warnings=ExperimentalWarning", "{self}/src/testee.ts"]},
  "generated": {
    "rendered": "{out}/ts-generated",
    "build": [],
    "run": {"argv": ["node", "--experimental-strip-types", "--no-warnings=ExperimentalWarning", "{rendered}/testee.ts"]}
  }
}
```

`toolchains` are looked up on the path before anything runs, and a missing
one fails the suite rather than skipping it, as every fixture in this
repository does. `build` is a list of commands run once, in order; `run` is
how a testee process starts. `{self}` is the directory of `testee.json`,
`{checkout}` the repository root, `{rendered}` where the runner renders the
probe and proof families for the generated testee, `{out}` a scratch directory of this
run's own under `conformance/.out`, removed when the run ends, `{exe}` the
platform's executable suffix. A rendering placed under `{out}` lies inside
the checkout, where a TypeScript package resolves `@nightseam/*` through
the workspace. `env` on any command is
merged over the runner's environment; `cwd` defaults to `{self}`.
