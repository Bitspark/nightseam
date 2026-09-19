# @nightseam/session

A session as a thing that outlives connections, over a tunnel's channels:
one up channel the machine speaks the family on, any number of down channels
the consumers speak it on in a role each, one holder of control, an ask
routed to the holder, and a log of every frame in one order, replayed from a
sequence. It is generic over the family: what a family governs reaches it
as the `decides` and `asks` the generator renders for its session tier.

```ts
import { Registry, memoryLog } from '@nightseam/session';
import { decides, asks } from '@example/chat-client';

const registry = new Registry();
registry.bind('s-1', up, { decides: m => decides.has(m), asks: m => asks.has(m) }, memoryLog(1 << 20));
const attachment = registry.attach('s-1', down, 'participant', 'consumer:7', 0);
registry.control('s-1', attachment);
registry.attention();     // every session with an unanswered ask

const stop = registry.onChange(change => {
  change.kind;            // bound, attached, ask_routed, frame_appended, …
});
stop();                   // that registration, and no other
```

Every domain change of the registry goes two ways. Through `onChange` — one
`Change` per change, carrying the session, whom it concerns, the method it
names, the sequence the log reached and the trace of the frame it concerns —
which is the hook a consumer builds its own events from, and the one place
such events are computed. And to the observer of the peer the session's up
channel runs over, as `session.bound`, `session.unbound`, `session.attached`,
`session.detached`, `ask.raised`, `ask.routed`, `ask.answered`,
`control.changed`, `frame.appended` and `session.refused`, declared into the
runtime's `ObserverEvents` so that a consumer's `switch (event.type)` covers
them beside the runtime's and the tunnel's. The session takes no observer of
its own.

Both say names, ids, origins, sequences and sizes, and never a payload:
`frame.appended` says which sequence, which direction, whose origin and how
many bytes, and what the frame carried stays in the log.

The rules it holds, one test each in both languages: a response reaches
the one consumer that asked and an event every attached one; a deciding
frame is the holder's alone, refused with `not_controlling` from anyone
else; an ask reaches the holder and follows a transfer while open; the
session's ids are its own across consumers that both mint `c:1`; a consumer
resumes from the log before any live frame; a frame's members the relay does
not know arrive with it; the machine's channel ending ends every consumer,
a consumer's only itself.

It has no authentication, no rule about who may take control or for how
long, no durable store and no lifecycle of its own: those are the
consumer's, called in. `Log` is the interface a durable store implements;
`memoryLog` is the one shipped. The full description is `docs/session.md`
in the repository.
