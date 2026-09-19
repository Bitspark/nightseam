# The session

A session is a thing with an identity that outlives connections: one **up**
connection, the machine's, over which the session's family is spoken; any
number of **down** connections, the consumers', each in a role; one holder
of control; an ask routed to the holder; and a log of every frame in one
order, replayed from a sequence. Each side is a connection of the seam —
which a channel of a tunnel is, and so are the seam's pipe and a bare
socket: the relay sends on it, receives from it and closes it, and asks
nothing about what multiplexed it. The tunnel is the answer where one
connection carries many sessions and is no requirement where it carries
one, so an in-process machine binds the pipe it already speaks the profile
over and a consumer over a bare socket attaches its own. The relay is
generic over the family: what a family governs reaches it as two
predicates the generator renders for a session tier — which methods
*decide*, and which of the client's methods the machine *asks*.

This page is what a session does on the wire, which every language's relay
is held to — over the channels of a tunnel and over the seam's pipe with no
tunnel at all, the suite run twice, which is what it means for the transport
to be none of the relay's business ([the session runs over any connection
of the seam](../decisions/the-session-runs-over-any-connection-of-the-seam.md)).
What a consumer calls is [the session's surface](../runtime/session.md).

## The boundary rule

Nightseam owns what can be stated in terms of the profile and the session
tier and is the same for every consumer — mechanism. A consumer owns what
names a concept of its own or decides a policy. So the relay routes a frame
by its kind and its method, mints the ids two consumers would collide on
and maps the responses back, holds who has control and re-routes the open
ask when it moves, and states what a log is. It has **no authentication, no
rule about who may take control or for how long, no durable store, and no
lifecycle of its own**: a consumer decides those and calls in.

## The roles

A consumer attaches in a role: a **participant**, which may decide while it
holds control and may be given it, or an **observer**, which never decides
and is never given control. It attaches as something — an origin, stamped
on every frame it sends as the log's — and from the last sequence it holds.
Control is given to one participant, or released. The observer role, a
consumer that never decides, is a different thing from an observer of
events, which watches traffic rather than taking part in a session at all.

## The rules

Held, one test each, in both languages:

1. **A response reaches the one consumer that asked; an event reaches
   every attached one.**
2. **A deciding frame is the holder's alone.** A request whose method
   decides, a cancel, and an answer to what the machine asked are refused
   with `not_controlling` — a response the consumer sees, the machine never
   does — from an observer, and from a participant that does not hold
   control.
3. **An ask reaches the holder and follows a transfer while open.** A
   request the machine sends whose method is asked is routed to the holder
   of control; when control moves before it is answered, it is routed again
   to the new holder, under the id the machine gave it, and the answer of
   the consumer control left is dropped. Releasing control leaves an open
   ask with nobody.
4. **The session's ids are its own.** Every peer mints `c:N` per connection,
   so two consumers attached over a session's life both send `c:1`. The
   relay is the family's client towards the machine and mints the ids it
   sends up itself, unique per session, keeping which consumer's request
   each stands for; the machine's own ids, `s:N`, travel down as they are
   ([the relay mints its own ids](../decisions/the-relay-mints-its-own-ids.md)).
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
9. **A message over the log's bound is replayed truncated** — the log keeps
   it cut and says so, and the relay delivers nothing for it (§ the log).
10. **A consumer is told who holds control and where it stands.**
    `session.control` reaches every attachment on attach, before the replay
    begins, and on every change; `session.cursor` reaches the one attachment
    a frame was just delivered to, naming that frame's place in the log.
    Neither is logged and neither may come from the machine — the session's
    own vocabulary, below.

## The session's own vocabulary

A session speaks its own vocabulary on the wire, as the tunnel speaks
`channel.open` and `channel.credit`: ordinary events of the profile,
carried on the same connection beside the family the session governs and
distinct from it, under the `session.` prefix the session tier reserves.
The peer forwards them and reads nothing into them, the relay produces them
and a consumer reads them. [How a layer speaks](vocabulary.md) is the test
that puts them here rather than in the profile's envelope, where a sequence
and a fifth kind were each tried for an afternoon.

| frame | data | who is sent it | when |
| --- | --- | --- | --- |
| `session.control` | `{"holder": "<origin>"}`, or `{"holder": null}` where nobody holds it | every attachment | once on attach, before the replay begins, and on every change — given, released, transferred |
| `session.cursor` | `{"sequence": N}` | the one attachment a frame was just delivered to | straight after each frame it is delivered, replay and live alike, in the same order |

Four rules hold of both ([the session's vocabulary is not
logged](../decisions/the-sessions-vocabulary-is-not-logged.md)):

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

Both are readable off a consumer's attachment, set from what the relay
sent, so a consumer reading the state and one reading the wire agree —
[the surface](../runtime/session.md#the-surface) says how. That is the whole
of resumption on the consumer's side: attaching again after the last cursor
it was told gives it exactly what came after the last frame it was
delivered, and nothing it already holds.

Two things of this vocabulary do not exist yet, and nothing above is undone
by them: `session.subscribe` and `session.unsubscribe`, by which a consumer
narrows which of the family's events reach it (#51), and the typed surface
by which these operations reach a family's generated code as operations no
family declares, with the generator refusing a family a method or an event
under the `session.` prefix (#50, #45).

## The log

Every frame of the family, up or down, takes a place in the log — a
sequence, ascending — under the origin of the consumer that sent it or
under the machine's, with the direction it travelled and when. A message
whose JSON is longer than the log's bound is kept as the text it was cut to
and marked so, so that one frame cannot grow a long-lived session without
bound; on replay the relay passes such a frame over. Replay delivers in
ascending sequence order, which is the whole of what a log owes beyond
storing frames: the relay holds the up connection across the append and the
send, so that what the log says the consumers sent is the order the machine
saw.

A session is bound at its log's head: the log is read once on the way in,
and the session seated at the last sequence that read delivered, so a
session bound after a restart stands at its log's end and a consumer
attaching with `after: 0` before the machine has spoken again is given
everything the log holds rather than nothing. The read happens before the
machine's connection is read and under the relay's own lock, so a frame
arriving while it runs is recorded above the head and never under a
sequence the log already gave out ([the log is bound at its
head](../decisions/the-log-is-bound-at-its-head.md)).

Which log a session has — one in memory that a restart forgets, or one over
whatever a consumer stores frames in — is the consumer's, and so are
retention, redaction and what a cut frame means for a reader, by the
boundary rule.

## What is refused, and how a connection ends

Two refusals travel on the wire, both as the profile's error object in a
response the consumer sees and the machine never does: `not_controlling`,
for a deciding frame from a consumer that does not hold control, and
`busy`, where the session already has as many requests open towards the
machine as it may (256 by default). Every other refusal is a call's, on the
consumer's server, and never a frame — [the
surface](../runtime/session.md#what-it-refuses-with) has them.

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
relay it attached to ([refusals are codes, not
prose](../decisions/refusals-are-codes-not-prose.md)) — and the machine's
connection carries them as a consumer's does. A machine that sends a frame of the session's own
vocabulary is **1002** as well, under the reason that names the frame. A
consumer that does not take its frames for one send deadline is detached; a
machine that does not ends the session. [The profile](profile.md#the-connection-beneath)
lists every close code.

## Observing it

A session tells its observer ten facts: a session bound and unbound, a
consumer attached and detached, an ask raised, routed and answered, control
moved, a frame appended to the log, and a consumer's frame refused. None
says a payload, and a refusal is a change and never a frame, because the
machine never saw it. Which observer that is follows from the connection
the machine speaks over — [the surface](../runtime/session.md#observing-it)
— and [the observer](../runtime/observer.md) has the rule and every event of
every layer.
