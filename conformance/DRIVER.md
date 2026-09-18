# The driver protocol

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
the op's arguments as further members. An answer is `{"id", "ok": …}` with
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
← {"id": 1, "ok": {"driver": 1, "language": "go", "layers": ["seam", "peer", "tunnel", "session"],
                   "features": ["listen", "pipe", "observer", "propagator", "lazy"]}}
```

`layers` names the op families the testee implements; a scenario names the
layers and features it `needs`, and the runner skips it for a testee that
lacks one. The features:

| feature | means |
|---|---|
| `listen` | the testee can accept a connection (`conn.listen`, `peer.listen`); a language whose runtime only dials lacks it and still runs both roles, through `peer.over` on a connection the other side accepted |
| `pipe` | `conn.pipe`, an in-process connected pair |
| `observer` | `observe: true` on a peer, and `peer.observed` |
| `propagator` | `propagate: true` on a peer: traces are minted and carried |
| `lazy` | `consume: "lazy"` on a connection |

Between scenarios the runner sends `reset`: the testee closes and forgets
everything it holds — every connection, peer, listener, registry — and
answers `{}` with nothing left running, so that one process serves a whole
run and no scenario sees another's state. The last request is `bye`; the
testee resets and exits 0. A testee that exits before `bye`, or writes a
line that is not an answer, fails the scenario it was in and is started
again for the next.

## Handles

Everything a testee makes is a **handle**, a string it mints and the runner
passes back as `on`: a connection, a listener, a peer, a tunnel, a channel,
a registry, an attachment, a call. A channel is also a connection, and takes
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
`tunnel.open`, `tunnel.accept`, `session.bind` and `session.attach`, each of
which can block on the peer process.

The testee is **pull-based**. It keeps, per handle, what arrived while the
runner was not asking: frames received on a connection, events received on
a peer, the lifecycle of every request a canned handler served, what an
observer was told, the changes a registry made. An `await_*` op returns the
first entry that matches, removing it; a `drain` op returns everything held
and empties it. Nothing is reported unasked and nothing is lost between
asks.

## Inbound consumption

A connection made with `"consume": "lazy"` receives nothing until
`conn.receive` asks; one made `"eager"` (the default) receives as frames
arrive and holds them. Lazy is what a scenario about credit needs: a channel
whose reader has stopped is the only way a sender stalls. A testee that
lacks `lazy` says so in `hello` and the runner skips those scenarios.

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
| `peer.listen` | `options` | `{"handle", "url"}` — a listener accepting one peer at `url`, as the server |
| `peer.accept` | **`on`** listener, `within_ms` | `{"handle"}` the accepted peer, with the listener's `options` |
| `peer.dial` | **`url`**, `options` | `{"handle"}` the client peer, connected |
| `peer.over` | **`on`** connection or channel, **`role`** `"client"`\|`"server"`, `options` | `{"handle"}` a peer speaking the profile over that connection |
| `peer.handle` | **`on`**, **`method`**, **`behavior`** | `{}` — registers a canned handler, see below |
| `peer.on_event` | **`on`**, **`name`**, `behavior` | `{}` — how an event is taken: `record` (default), `block`, `panic` |
| `peer.call` | **`on`**, **`method`**, `params`, `timeout_ms` | `{"handle"}` a call, in flight |
| `call.await` | **`on`**, `within_ms` | `{"result": …}` or `{"error": {"code", "message", "data"}}` |
| `call.cancel` | **`on`** | `{}` — the caller gives up; its `call.await` then ends `cancelled` |
| `peer.emit` | **`on`**, **`event`**, `data`, `within_ms` | `{}` |
| `peer.await_event` | **`on`**, **`name`**, `within_ms` | `{"data": …}` |
| `peer.await_request` | **`on`**, **`method`**, **`phase`** `"started"`\|`"ended"`, `within_ms` | `{"id", "method", "phase", "outcome"}` — what a canned handler saw; `outcome` on `ended` is `ok`, `error`, `cancelled` or `panic` |
| `peer.observed` | **`on`**, `trace` (bool), `drain` (bool, default true) | `[event, …]` what the observer was told, normalized, see below |
| `peer.close` | **`on`** | `{}` |
| `peer.await_close` | **`on`**, `within_ms` | `{"clean": bool}` — clean when this side or the other closed it by choice; otherwise the peer ended on an error, the transport's or its own |

`peer.await_close` reports no code: a peer that refuses a frame ends the
connection as its language does — an abort, a close with a code of its own —
and what the scenario holds is that it ended, not how its transport said so.
The seam's `conn.await_close` is where a code is a fact.

`options` on `peer.listen`, `peer.dial` and `peer.over`:

| member | default | means |
|---|---|---|
| `max_frame_bytes` | the runtime's | a frame over it is refused and the connection ended |
| `queue_capacity` | the runtime's | inbound events held before the peer is stalled |
| `request_timeout_ms` | the runtime's | a call's deadline |
| `write_timeout_ms` | the runtime's | how long a send may wait |
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
| `{"kind": "panic", "value": "…"}` | panics, throws — whatever the language does when a handler gives up — with that value |
| `{"kind": "reverse", "method", "params"}` | calls `method` on the remote **from the request's own context**, so the call is a child of the request's trace, with `params` or, absent, its own; answers with what came back, or fails with what came back |
| `{"kind": "emit", "event", "data", "then": …}` | emits `event` from the request's context, then answers `then` |

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
| a tunnel's or a session's | as its layer says, below |

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
| `tunnel.over` | **`on`** peer, `options` (`window`, `max_frame_bytes`, `accept_capacity`) | `{"handle"}` |
| `tunnel.open` | **`on`**, **`family`**, `after`, `consume`, `within_ms` | `{"handle", "id"}` — the channel, a connection handle |
| `tunnel.accept` | **`on`**, `consume`, `within_ms` | `{"handle", "id", "family", "after"}` |

A channel takes every `conn.*` op, and `peer.over` makes a peer of it. A
tunnel observes through its peer's observer; its events reach
`peer.observed` on that peer: `channel.opened` (`family`, `id`, `after`,
`opener`), `channel.accepted` (`family`, `id`, `after`), `channel.closed`
(`family`, `id`, `code`, `reason`), `credit.stall` (`family`, `id`,
`waiting`), `open.refused` (`family`, `reason`).

### Session — `session.*`, `attachment.*`

The session component: `session/go`, `@nightseam/session`.

| op | arguments | answer |
|---|---|---|
| `session.new` | `options` (`max_attachments`, `max_inflight`) | `{"handle"}` a registry |
| `session.bind` | **`on`**, **`id`**, **`channel`**, **`governance`** `{"decides": […], "asks": […]}`, `log` `{"max_frame_bytes"}`, `within_ms` | `{}` |
| `session.attach` | **`on`**, **`id`**, **`channel`**, **`role`** `"participant"`\|`"observer"`, **`origin`**, `after`, `within_ms` | `{"handle"}` an attachment; the replay has been delivered when it answers |
| `session.control` | **`on`**, **`id`**, `attachment` (a handle, or `null` to release) | `{}` |
| `session.attention` | **`on`** | `["id", …]` |
| `session.changes` | **`on`**, `trace`, `drain` | `[change, …]` normalized: `kind` (`bound`, `unbound`, `attached`, `detached`, `ask_raised`, `ask_routed`, `ask_answered`, `control_changed`, `frame_appended`, `refused`), `session`, and of `origin`, `role`, `sequence`, `method`, `trace` what the change carries |
| `session.await_change` | **`on`**, **`kind`**, `within_ms` | the first such change, removed |
| `attachment.detach` | **`on`** | `{}` |

A session observes through the peer its machine's channel runs over; its
events reach `peer.observed` there: `session.bound`, `session.unbound`
(`code`, `reason`), `session.attached` (`role`, `origin`, `after`),
`session.detached` (`role`, `origin`), `ask.raised` (`id`, `method`,
`asking`), `ask.routed` (`id`, `method`, `origin`), `ask.answered` (`id`,
`method`, `origin`), `control.changed` (`origin`, `held`), `frame.appended`
(`sequence`, `direction`, `origin`, `bytes`, `method`), `session.refused`
(`code`, `method`, `role`, `origin`) — each with `session`, and `trace` when
asked for and the frame carried one.

### Generated code — `gen.*`, `client.*`

A language's second testee links the packages the generator renders for the
corpus's `probe` family — its client and its binding — over that language's
runtime. The runner renders `probe` and builds this testee from the recipe
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
| `client.close` | **`on`** | `{}` |
| `gen.validate` | **`type`**, **`value`** | `{"valid": true}` or `{"valid": false, "message"}` — the protocol package's validator on a type expression |
| `gen.decides` / `gen.asks` | **`method`** | `{"value": bool}` |
| `gen.conversation` | | `{"event", "path"}` |
| `gen.errors` | | `["code", …]` the family's public errors, sorted |

The canned server: `echo` answers the payload with `text` reversed;
`no_args` answers `"none"`; `seen` answers `[]`; `reverse` on a client
answers the payload with `text` prefixed by the language's name and a colon,
`"go:"`, `"typescript:"`; a `changed` event is held for `client.await_changed`
and a `noticed` event on the server for `peer.await_event` on `gen.serve`'s
handle. The scenario knows the language on each side and holds the prefix.

## `testee.json`

A language joins the suite with `conformance/<lang>/testee.json`:

```json
{
  "language": "typescript",
  "toolchains": ["node"],
  "build": [],
  "run": {"argv": ["node", "--experimental-strip-types", "{self}/src/testee.ts"]},
  "generated": {
    "rendered": "{self}/.generated",
    "build": [],
    "run": {"argv": ["node", "--experimental-strip-types", "{rendered}/testee.ts"]}
  }
}
```

`toolchains` are looked up on the path before anything runs, and a missing
one fails the suite rather than skipping it, as every fixture in this
repository does. `build` is a list of commands run once, in order; `run` is
how a testee process starts. `{self}` is the directory of `testee.json`,
`{checkout}` the repository root, `{rendered}` where the runner renders the
probe family for the generated testee, `{out}` a scratch directory kept for
the run, `{exe}` the platform's executable suffix. `env` on any command is
merged over the runner's environment; `cwd` defaults to `{self}`.
