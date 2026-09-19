# The session's surface

`session/go` and `@nightseam/session` are the runtime component that holds
a session — the relay, the registry, the holder of control, the log, and
the changes it reports — beside the tunnel, generic over the family: what a
family governs reaches it as the two functions the generator renders for a
session tier, `Decides` and `Asks`. Both are held to one suite —
`session/go/sessiontest`, `session/ts/src/conformance.ts` — governed by the
generator's own probe family as the corpus declares it, and to each other
over a real socket; the suite is run twice in each language, once over the
channels of a tunnel and once over the seam's pipe with no tunnel at all.
This page is the surface in both languages; what a session does on the wire
is [the session](../wire/session.md).

## The surface

```go
registry, err := session.New(session.Options{})
if err != nil {
    return err
}
registry.Bind(id, up, session.Governance{Decides: chatclient.Decides, Asks: chatclient.Asks}, session.NewMemoryLog(1<<20))
attachment, _ := registry.Attach(id, down, session.Participant, "consumer:7", after)
attachment.Holder()                // who holds control, as this consumer was told
attachment.Sequence()              // where in the log it stands, which it reattaches after
registry.Control(id, attachment)   // or nil, releasing control
registry.Attention()               // every session with an unanswered ask
```

```ts
const registry = new Registry();
registry.bind('s-1', up, { decides: m => decides.has(m), asks: m => asks.has(m) }, memoryLog(1 << 20));
const attachment = registry.attach('s-1', down, 'participant', 'consumer:7', 0);
attachment.holder;                 // string | null
attachment.sequence;
registry.control('s-1', attachment);   // or null
registry.attention();
```

`Bind` gives a session its own connection, the family's governance and its
log; it is live from then until that connection closes, and learns the log's
head on the way in, so that a log bound with frames already in it is bound
at its head rather than at nothing. `Attach` adds a consumer over a
connection of its own, in a role — `session.Participant` or
`session.Observer` in Go, `'participant'` or `'observer'` in TypeScript —
saying what the consumer is, stamped on every frame it sends as the log's
`Origin`, and the last sequence it holds. `Control` gives one participant
control, or releases it. Who may attach, who may take control and for how
long are checked before calling.

What the relay told a consumer is readable off its attachment, set from
what was sent, so a consumer reading the state and one reading the wire
agree: Go `Attachment.Holder() (origin string, held bool)`,
`Attachment.Sequence() int64` and `Attachment.OnControl(func(origin string,
held bool)) (stop func())`; TypeScript `attachment.holder` (`string |
null`), `attachment.sequence` and `attachment.onControl(fn): () => void`. A
registration is not called with the state the consumer joined at — that
frame is sent before `Attach` returns, which is before there is anywhere to
call — and `Holder` reads it instead. `Detach` and `detach` end a consumer's
attachment from this side.

## Options

The registry is configured by the value passed where it is made:
`session.Options`, to `session.New`, in Go; `RegistryOptions`, to `new
Registry(...)`, in TypeScript. Every member is optional and a member left
out takes the default — zero in Go, absent in TypeScript, and `new
Registry()` with nothing at all is the default registry.

| Go | TypeScript | default | meaning |
| --- | --- | --- | --- |
| `MaxAttachments` | `maxAttachments` | 64 | consumers on one session; an attach beyond it is refused with `too_many_attachments` |
| `MaxInflight` | `maxInflight` | 256 | requests open towards the machine; one beyond it is answered `busy` |
| `SendTimeout` | — | 10s | Go only: how long a frame may wait for a connection; a consumer that does not take its frames is detached, a machine that does not ends the session — a Go send taking a context it can wait on where the TypeScript connection's `send` hands the frame over and returns, leaving nothing to bound |
| `Observer` | `observer` | none | where a session tells what it does when its machine's connection observes through nothing of its own (§ observing it) |

Go's `session.New(options) (*Registry, error)` refuses a negative
`MaxAttachments`, `MaxInflight` or `SendTimeout` with a nil registry and a
`*session.Error` whose code is `invalid_options`; zero still selects the
default. TypeScript refuses an explicitly supplied limit that is not a
positive integer, including zero, with `invalid_options`. What a registry
settled on is readable back in TypeScript, `registry.limit`.

## The log

```go
type Frame struct {
    Sequence  int64
    Direction Direction   // Up: towards the machine, what a consumer sent; Down: from it
    Origin    string      // what the consumer was attached as, or the machine's
    At        time.Time
    Message   json.RawMessage
    Truncated bool        // the message exceeded the log's bound and is kept cut
}
type Log interface {
    Append(ctx context.Context, frame Frame) (sequence int64, err error)
    Replay(ctx context.Context, after int64, deliver func(Frame) error) error
}
// An optional capability of a Log; Bind prefers it to Replay.
type Header interface {
    Head(ctx context.Context) (int64, error)
}
```

`NewMemoryLog(maxFrameBytes)` in Go and `memoryLog(maxFrameBytes)` in
TypeScript are the one the package ships: a log in memory, bounded per
frame, where a message whose JSON is longer than the bound is kept as the
text it was cut to and replayed with `Truncated` set. Go reads zero or less
as unbounded, which only a process that ends soon may ask for; TypeScript
takes a positive integer and refuses anything else with `invalid_options`,
so a bound is chosen there rather than defaulted — `memoryLog(1 << 20)` is
the package README's.

It is the log for a session that need not outlive the process: nothing of it
is written down, and a registry that restarts replays nothing. A log that
must outlive one is the consumer's own `Log` — the interface above, over
whatever it stores frames in — passed to `Bind` in its place; nothing else
changes. `Replay` delivers in ascending sequence order, which is the whole of
what a durable implementation owes beyond storing frames, and is what
`Bind`'s fallback read relies on to seat the session at the log's head
([the log, on the wire](../wire/session.md#the-log)). A log that knows its
head without a read implements Go `Header` or the optional TypeScript
`Log.head(): Promise<number>`. The result is its last assigned sequence,
zero for an empty log. Both memory logs provide this lookup without replay.
The lookup runs before processing any machine frames, as the fallback does.

A failed lookup does not fall back to replay or start from zero: Go `Bind`
returns `session_invalid`; TypeScript's queued initialization ends the
session and closes its connections with 1011. A negative head is invalid;
TypeScript also requires a safe integer.

## What it refuses with

A consumer's server drives this layer by calling into it, so what a call
refuses with is part of the surface it is written against: a code a program
branches on, never prose it would have to match ([refusals are codes, not
prose](../decisions/refusals-are-codes-not-prose.md)). There is one
vocabulary and it is the same in both languages, name for name.

In Go a refusal is a `*session.Error` with `Code` and `Message`, reached by
either of the ways Go asks —

```go
var refusal *session.Error
if errors.As(err, &refusal) && refusal.Code == session.ErrorNoSession { … }

if errors.Is(err, &session.Error{Code: session.ErrorNoSession}) { … }
```

— `Is` matching by code alone, since a message is the part of a refusal that
may be reworded. In TypeScript it is a `DuplexError` whose `code` is the
same string. Every code is a constant: `session.ErrorNoSession` and its nine
neighbours in Go.

| code | what it refuses |
| --- | --- |
| `invalid_options` | a negative `MaxAttachments`, `MaxInflight` or `SendTimeout` in Go; a supplied `maxAttachments` or `maxInflight` that is not a positive integer in TypeScript; and, in Go, a replay given nowhere to deliver |
| `no_session` | no session is bound under that id, or the one that was has ended: `Attach` and `Control` both |
| `not_attached` | control given to a consumer that is not attached to this session, one of another's or one that has left |
| `not_controlling` | control given to an observer — and, on the wire, a deciding frame from a consumer that does not hold control |
| `origin_invalid` | an origin that is not text; TypeScript only, an origin being a `string` in Go and unable to be anything else |
| `role_invalid` | a consumer attaching as something that is neither participant nor observer |
| `sequence_invalid` | an `after` that is no sequence |
| `session_exists` | a bind under an id already bound |
| `session_invalid` | a bind without an id, and in Go without a connection, without the family's `Decides` and `Asks`, without a log, or with a log that could not be read; an attach without a connection |
| `too_many_attachments` | an attach beyond `MaxAttachments` |

`busy` is the one refusal no call returns: the relay answers a consumer's
request with it, as the profile's error object, where the session already
has as many open towards the machine as it may. `not_controlling` travels
both ways — from a call that gave an observer control, and on the wire to a
consumer that decided without holding it. How a connection ends, and with
which code and reason, is [the wire's](../wire/session.md#what-is-refused-and-how-a-connection-ends).

## What a consumer builds on it

A consumer's server: its attach operation opens the consumer's connection —
a channel through the tunnel where one socket carries several, the socket
itself where it carries one — and calls `Attach`; the tunnel over an
accepted peer is made in `runtime.Options.Prepare` in Go and before `attach`
in TypeScript, so that a consumer's first `channel.open` meets it rather
than a peer still being furnished ([the tunnel](tunnel.md#making-one-and-when));
its rule for who may take control — and a lease, if it wants one — calls
`Control`; its frame log is a durable `Log`; its attention list is
`Attention()`; its machine side hands `Bind` a connection per running
session, a channel it opened and named in a report where the machine is
elsewhere, and where the machine is this process the near end of a pipe it
speaks the profile over itself. Nothing of that is in this package, by the
boundary rule.

## Observing it

Beside the changes its registry reports — `Registry.OnChange(fn)` in Go,
`registry.onChange(fn)` in TypeScript, each handing back the stop that ends
that registration alone — a session tells its observer the same ten facts:
a session bound and unbound, a consumer attached and detached, an ask
raised, routed and answered, control moved, a frame appended to the log, and
a consumer's frame refused. Neither says a payload. [The
observer](observer.md) has the rule and every event of every layer.

Which observer that is follows from the connection the session's machine
speaks over, in this order:

1. **The peer it runs over**, where it runs over one — a tunnel channel,
   which reports `Peer()` in Go and carries `observe` in TypeScript. That
   peer's observer is the one the consumer already chose, and a session over
   such a connection never falls back to the registry's, a peer given no
   observer observing nothing.
2. **`Options.Observer` / `RegistryOptions.observer`**, where the connection
   runs over no peer: the seam's pipe, a bare socket, an in-process machine.
   Without it the ten facts of such a session would be lost, which is fine
   for a pipe in a test and is not for a service whose own process is the
   machine.
3. **Nothing**, where there is neither — the no-op an observer already
   means, not a failure.
