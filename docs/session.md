# The session

A session is a thing with an identity that outlives connections: one **up**
channel, the machine's, over which the session's family is spoken; any
number of **down** channels, the consumers', each in a role; one holder of
control; an ask routed to the holder; and a log of every frame in one order,
replayed from a sequence. `session/go` and `@nightseam/session` are the
runtime component that holds it, beside the tunnel, generic over the family:
what a family governs reaches it as the two functions the generator renders
for a session tier, `Decides` and `Asks`.

Both are held to one suite — `session/go/sessiontest`,
`session/ts/src/conformance.ts` — governed by the generator's own probe
family as the corpus declares it, and to each other over a real socket.

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
registry.Bind(id, up, session.Governance{Decides: chatclient.Decides, Asks: chatclient.Asks}, session.NewMemoryLog(0))
attachment, _ := registry.Attach(id, down, session.Participant, "consumer:7", after)
registry.Control(id, attachment)   // or nil, releasing control
registry.Attention()               // every session with an unanswered ask
```

`Bind` gives a session its own channel, the family's governance and its
log; it is live from then until that channel closes. `Attach` adds a
consumer over a channel of its own, in a role — `Participant`, which may
decide while it holds control and may be given it, or `Observer`, which
never decides and is never given control — saying what the consumer is,
stamped on every frame it sends as the log's `Origin`, and the last sequence
it holds. `Control` gives one participant control, or releases it. Who may
attach, who may take control and for how long are checked before calling.

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
   cut is passed over, a cut message being no message for a channel that
   speaks the family.
6. **A frame carrying members the relay does not know arrives with them.**
   The relay reads a frame as a JSON object and rewrites its `id` alone;
   every other member — a trace context, a member of a later profile —
   reaches the other side verbatim, in the place it arrived in.
7. **The machine's channel ends every consumer, a consumer's only itself.**
   The up channel closing ends every attached channel with the same code and
   reason; a consumer's channel closing detaches it and nothing else.
8. **Attention is every session the machine asked of** and nobody has
   answered, by id, in order.
9. **A message over the log's bound is replayed truncated.**

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

`NewMemoryLog(maxFrameBytes)` is the one the package ships. What the log
says the consumers sent is the order the machine saw: the relay holds the
up channel across the append and the send. A durable store — and retention,
redaction, what `Truncated` means for a reader — is the consumer's, behind
the interface.

## Errors and limits

| | |
| --- | --- |
| `not_controlling` | a deciding frame from a consumer that does not hold control |
| `busy` | a request beyond the ones the session may have open towards the machine |
| `MaxAttachments` | 64 consumers on one session; an attach beyond it is refused |
| `MaxInflight` | 256 requests open towards the machine |
| `SendTimeout` | 10 seconds a frame may wait for a channel; a consumer that does not take its frames is detached, a machine that does not ends the session |

## What a consumer builds on it

A consumer's server: its attach operation opens the consumer's channel
through the tunnel and calls `Attach`; its rule for who may take control —
and a lease, if it wants one — calls `Control`; its frame log is a durable
`Log`; its attention list is `Attention()`; its machine side opens a channel
per running session and names the handle in a report, which the server
passes to `Bind`. Nothing of that is in this package, by the boundary rule.

## Observing it

Beside the changes its registry reports — `Registry.OnChange(fn)` in Go,
`registry.onChange(fn)` in TypeScript, each handing back the stop that ends
that registration alone — a session tells the observer of the peer its
machine speaks over the same ten facts: a session bound and unbound, a
consumer attached and detached, an ask raised, routed and answered, control
moved, a frame appended to the log, and a consumer's frame refused. Neither
says a payload. [observability.md](observability.md) has the rule and every
event of every layer.

The `Observer` role above — a consumer that never decides — is a different
thing from an observer of events, which watches traffic rather than taking
part in a session at all.
