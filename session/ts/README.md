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
registry.bind('s-1', up, { decides, asks }, memoryLog(1 << 20));
const attachment = registry.attach('s-1', down, 'participant', 'consumer:7', 0);
registry.control('s-1', attachment);
registry.attention();     // every session with an unanswered ask
```

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
