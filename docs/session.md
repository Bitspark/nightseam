# The session

A session is a thing with an identity that outlives connections: one **up**
connection, the machine's, over which the session's family is spoken; any
number of **down** connections, the consumers', each in a role; one holder
of control; an ask routed to the holder; and a log of every frame in one
order, replayed from a sequence. Each side is a connection of the seam —
`duplex.Conn` in Go, `FrameConnection` in TypeScript — which a channel of a
tunnel is, and so are the seam's pipe and a bare socket: the relay sends on
it, receives from it and closes it, and asks nothing about what multiplexed
it. The tunnel is the answer where one connection carries many sessions and
is no requirement where it carries one, so an in-process machine binds the
pipe it already speaks the profile over and a consumer over a bare socket
attaches its own. `session/go` and `@nightseam/session` are the runtime
component that holds it, beside the tunnel, generic over the family: what a
family governs reaches it as the two functions the generator renders for a
session tier, `Decides` and `Asks`.

Both are held to one suite — `session/go/sessiontest`,
`session/ts/src/conformance.ts` — governed by the generator's own probe
family as the corpus declares it, and to each other over a real socket. The
suite is run twice in each language, once over the channels of a tunnel and
once over the seam's pipe with no tunnel at all, which is what it means for
the transport to be none of the relay's business.

## The boundary rule

Nightseam owns what can be stated in terms of the profile and the session
tier and is the same for every consumer — mechanism. A consumer owns what
names a concept of its own or decides a policy. So the component routes a
frame by its kind and its method, mints the ids two consumers would collide
on and maps the responses back, holds who has control and re-routes the open
ask when it moves, and states what a log is and ships one in memory. It has
**no authentication, no rule about who may take control or for how long, no
durable store, and no lifecycle of its own**: a consumer decides those and
calls in.

## The surface

```go
registry := session.New(session.Options{})
registry.Bind(id, up, session.Governance{Decides: chatclient.Decides, Asks: chatclient.Asks}, session.NewMemoryLog(1<<20))
attachment, _ := registry.Attach(id, down, session.Participant, "consumer:7", after)
attachment.Holder()                // who holds control, as this consumer was told
attachment.Sequence()              // where in the log it stands, which it reattaches after
registry.Control(id, attachment)   // or nil, releasing control
registry.Attention()               // every session with an unanswered ask
```

`Bind` gives a session its own connection, the family's governance and its
log; it is live from then until that connection closes, and it reads the log
once on the way in, so that a log bound with frames already in it is bound
at its head rather than at nothing. Which log is the consumer's:
`session.NewMemoryLog(maxFrameBytes)` in Go and `memoryLog(maxFrameBytes)`
in TypeScript are the one the package ships, and *The log* below says what
is passed in their place when a session's frames must outlive the process.
`Attach` adds a consumer over a connection of its own, in a role —
`Participant`, which may decide while it holds control and may be given it,
or `Observer`, which never decides and is never given
control — saying what the consumer is, stamped on every frame it sends as
the log's `Origin`, and the last sequence it holds. `Control` gives one
participant control, or releases it. Who may attach, who may take control
and for how long are checked before calling.

The registry itself is configured by the value passed where it is made:
`session.Options`, to `session.New`, in Go; `RegistryOptions`, to
`new Registry(...)`, in TypeScript. Every member of it is optional and a
member left out takes the default — zero or less in Go, absent in
TypeScript, and `new Registry()` with nothing at all is the default
registry. `MaxAttachments` and `MaxInflight` — `maxAttachments` and
`maxInflight` — are 64 and 256, the two the table below gives; `Observer`
and `observer` are where a session tells what it does when the connection
its machine speaks over observes through nothing of its own, which
*Observing it* below states; Go's `SendTimeout` is ten seconds and has no
TypeScript member, a Go send taking a context it can wait on where the
TypeScript connection's `send` hands the frame over and returns, leaving
nothing to bound. What a
registry settled on is readable back in TypeScript, `registry.limit`, and
a member that is not a positive integer is refused there with
`invalid_options` where Go takes it for the default.

## The rules

Held, one test each, in both languages:

1. **A response reaches the one consumer that asked; an event reaches
   every attached one.**
2. **A deciding frame is the holder's alone.** A request whose method
   `Decides`, a cancel, and an answer to what the machine asked are refused
   with `not_controlling` — a response the consumer sees, the machine never
   does — from an observer, and from a participant that does not hold
   control.
3. **An ask reaches the holder and follows a transfer while open.** A
   request the machine sends whose method `Asks` is routed to the holder of
   control; when control moves before it is answered, it is routed again to
   the new holder, under the id the machine gave it, and the answer of the
   consumer control left is dropped. Releasing control leaves an open ask
   with nobody.
4. **The session's ids are its own.** Every peer mints `c:N` per connection,
   so two consumers attached over a session's life both send `c:1`. The
   relay is the family's client towards the machine and mints the ids it
   sends up itself, unique per session, keeping which consumer's request
   each stands for; the machine's own ids, `s:N`, travel down as they are.
5. **A consumer resumes from the log before any live frame.** A consumer
   attached with `after` receives the log's frames after that sequence
   first, then what arrives live, in one order and once; a frame the log
   cut is passed over, a cut message being no message for a connection that
   speaks the family. A log bound with frames already in it is bound at its
   head, so what it held before the session was bound is among them.
6. **A frame carrying members the relay does not know arrives with them.**
   The relay reads a frame as a JSON object and rewrites its `id` alone;
   every other member — a trace context, a member of a later profile —
   reaches the other side verbatim, in the place it arrived in.
7. **The machine's connection ends every consumer, a consumer's only
   itself.** The up connection closing ends every attached connection with
   the same code and reason; a consumer's connection closing detaches it and
   nothing else.
8. **Attention is every session the machine asked of** and nobody has
   answered, by id, in order.
9. **A message over the log's bound is replayed truncated.**
10. **A consumer is told who holds control and where it stands.**
    `session.control` reaches every attachment on attach, before the replay
    begins, and on every change; `session.cursor` reaches the one attachment
    a frame was just delivered to, naming that frame's place in the log.
    Neither is logged and neither may come from the machine — *The session's
    own vocabulary* below.

## The session's own vocabulary

A session speaks its own vocabulary on the wire, as the tunnel speaks
`channel.open` and `channel.credit`: ordinary events of the profile,
carried on the same connection beside the family the session governs and
distinct from it, under the `session.` prefix the session tier reserves.
The peer forwards them and reads nothing into them, the relay produces them
and a consumer reads them. [layers.md](layers.md) is the test that puts them
here rather than in the profile's envelope, where a sequence and a fifth
kind were each tried for an afternoon.

| frame | data | who is sent it | when |
| --- | --- | --- | --- |
| `session.control` | `{"holder": "<origin>"}`, or `{"holder": null}` where nobody holds it | every attachment | once on attach, before the replay begins, and on every change — given, released, transferred |
| `session.cursor` | `{"sequence": N}` | the one attachment a frame was just delivered to | straight after each frame it is delivered, replay and live alike, in the same order |

Four rules hold of both:

- **Neither is logged.** The log holds the family's frames and nothing else,
  so a replay never gives a stale holder or a cursor of its own: a consumer
  that reattaches is told both afresh, by the relay, where it now stands.
- **Neither is a family event.** No family declares them, so a generated
  client sees an event it has no listener for and drops it, which is what
  the profile says a peer does with any event it did not declare. A consumer
  that wants them today listens on the name.
- **The machine never sends one.** The vocabulary is the relay's to produce;
  a machine that sends any `session.*` frame speaks for the layer above it,
  and its connection is ended with 1002 and a reason naming the frame —
  which ends every consumer of that session, as any close of the machine's
  connection does. The frame reaches neither the log nor a consumer.
- **The cursor is the log's sequence, not a count.** A frame the log cut is
  delivered as nothing and carries no cursor; the cursor after the next
  frame names that frame's own sequence, so a consumer that counted what
  arrived would stand one short after every truncation. A frame with no
  place in the log carries none either — the relay's own refusal, an ask
  handed again as control moves — so a cursor never moves backwards.

Both are readable off the attachment, set from what the relay sent, so a
consumer reading the state and one reading the wire agree: Go
`Attachment.Holder() (origin string, held bool)`, `Attachment.Sequence()
int64` and `Attachment.OnControl(func(origin string, held bool)) (stop
func())`; TypeScript `attachment.holder` (`string | null`),
`attachment.sequence` and `attachment.onControl(fn): () => void`. A
registration is not called with the state the consumer joined at — that
frame is sent before `Attach` returns, which is before there is anywhere to
call — and `Holder` reads it instead.

That is the whole of resumption on the consumer's side: attaching again
after the last cursor it was told gives it exactly what came after the last
frame it was delivered, and nothing it already holds.

Two things of this vocabulary are not here yet, and nothing above is undone
by them: `session.subscribe` and `session.unsubscribe`, by which a consumer
narrows which of the family's events reach it (#51); and the typed surface —
how these two reach a family's generated code as operations no family
declares, and the `session.` prefix the generator refuses a family to
declare a method or an event under (#50 and #45's other half, which waits on
#62). Both are 0.4.0.

## The log

```go
type Frame struct {
    Sequence  int64
    Direction Direction   // Up: from the machine; Down: from a consumer
    Origin    string      // what the consumer was attached as, or the machine's
    At        time.Time
    Message   json.RawMessage
    Truncated bool        // the message exceeded the log's bound and is kept cut
}
type Log interface {
    Append(ctx context.Context, frame Frame) (sequence int64, err error)
    Replay(ctx context.Context, after int64, deliver func(Frame) error) error
}
```

`NewMemoryLog(maxFrameBytes)` in Go and `memoryLog(maxFrameBytes)` in
TypeScript are the one the package ships: a log in memory, bounded per
frame, where a message whose JSON is longer than the bound is kept as the
text it was cut to and replayed with `Truncated` set, so that one frame
cannot grow a long-lived session without bound. Go reads zero or less as
unbounded, which only a process that ends soon may ask for; TypeScript
takes a positive integer and refuses anything else with `invalid_options`,
so a bound is chosen there rather than defaulted — `memoryLog(1 << 20)` is
the package README's.

It is the log for a session that need not outlive the process: nothing of it
is written down, and a registry that restarts replays nothing. A log that
must outlive one is the consumer's own `Log` — the interface above, over
whatever it stores frames in — passed to `Bind` in its place; nothing else
changes, the relay holding the up connection across the append and the send
either way, so that what the log says the consumers sent is the order the
machine saw. Retention, redaction and what `Truncated` means for a reader
are the consumer's with it, by the boundary rule.

Such a log is bound at its head. `Bind` reads it once, through `Replay` from
after zero, and seats the session at the last sequence that read delivered —
which is why `Replay` delivers in ascending sequence order, and is the whole
of what a durable implementation owes beyond storing frames. So a session
bound after a restart stands at its log's end, and a consumer attaching with
`after: 0` before the machine has spoken again is given everything the log
holds rather than nothing. The read happens before the machine's channel is
read and under the relay's own lock, so a frame arriving while it runs is
recorded above the head and never under a sequence the log already gave out.
A log that knows its head without a read may one day say so, as something
the relay prefers where a log has it; every `Log` above stays what it is.

## What it refuses with

A consumer's server drives this layer by calling into it, so what a call
refuses with is part of the surface it is written against: a code a program
branches on, never prose it would have to match. There is one vocabulary and
it is the same in both languages, name for name.

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
| `invalid_options` | a limit that is not a limit — `maxAttachments` or `maxInflight` in TypeScript, where Go's `Options` reads zero or less as the default — and, in Go, a replay given nowhere to deliver |
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
consumer that decided without holding it.

A close is not a refusal and carries a code of its own. A frame of the wrong
kind ends the connection it arrived on with **1003**, unsupported data, and
the reason `a session speaks JSON text frames`, in both languages and on
either connection: a consumer's own ends that consumer and leaves the
session standing, the machine's ends the session and every consumer with it.
Text that is no message of the profile is the fault beside it and ends the
connection with **1002**, a protocol error, under a reason naming what was
wrong with the frame: `a session frame must be a JSON object`, `duplicate
session frame member "id"`, `invalid trailing session frame content`, which
is the whole of the set a relay gives. Code and reason are one rule in both
languages — a consumer reading a close cannot ask which runtime wrote the
relay it attached to — and the machine's connection carries them as a
consumer's does. A machine that sends a frame of the session's own
vocabulary is **1002** as well, under the reason that names the frame.
[profile.md](profile.md) lists every close code.

## Limits

| | |
| --- | --- |
| `MaxAttachments` / `maxAttachments` | 64 consumers on one session; an attach beyond it is refused with `too_many_attachments` |
| `MaxInflight` / `maxInflight` | 256 requests open towards the machine; one beyond it is answered `busy` |
| `SendTimeout` | Go only: 10 seconds a frame may wait for a connection; a consumer that does not take its frames is detached, a machine that does not ends the session |

## What a consumer builds on it

A consumer's server: its attach operation opens the consumer's connection —
a channel through the tunnel where one socket carries several, the socket
itself where it carries one — and calls `Attach`; the tunnel over an
accepted peer is made in `runtime.Options.Prepare` in Go and before `attach`
in TypeScript, so that a consumer's first `channel.open` meets it rather
than a peer still being furnished (docs/tunnel.md); its rule for who may take
control — and a lease, if it wants one — calls `Control`; its frame log is a
durable `Log`; its attention list is `Attention()`; its machine side hands
`Bind` a connection per running session, a channel it opened and named in a
report where the machine is elsewhere, and where the machine is this process
the near end of a pipe it speaks the profile over itself. Nothing of that is
in this package, by the boundary rule.

## Observing it

Beside the changes its registry reports — `Registry.OnChange(fn)` in Go,
`registry.onChange(fn)` in TypeScript, each handing back the stop that ends
that registration alone — a session tells its observer the same ten facts:
a session bound and unbound, a consumer attached and detached, an ask
raised, routed and answered, control moved, a frame appended to the log, and
a consumer's frame refused. Neither says a payload.
[observability.md](observability.md) has the rule and every event of every
layer.

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

The `Observer` role above — a consumer that never decides — is a different
thing from an observer of events, which watches traffic rather than taking
part in a session at all.
