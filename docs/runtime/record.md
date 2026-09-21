# Recording and following a Wire

`duplex.Record` in Go and `record` from `@nightseam/duplex` in TypeScript
compose an ordinary [Wire](wire.md) with consumer-owned storage. Each admitted
message is appended in order and then sent to the original target. A follower
replays the stored messages after its cursor and then receives later appends.
The composition interprets neither payloads nor profile correlation.

```go
log := duplex.NewMemoryWireLog()
recorded, err := duplex.Record(ctx, target, log, duplex.RecordOptions{
    MaxQueuedMessages: 64,
})
if err != nil { return err }
defer recorded.Close(1000, "done")
follower, err := recorded.Follow(ctx, after, subscriber)
if err != nil { return err }
defer follower.Close()
// follower.Head is the replay boundary. follower.Done includes cleanup;
// follower.Err() reports a failure after admission.
```

```ts
const log = new MemoryWireLog();
const recorded = await record(target, log, { maxQueuedMessages: 64 }, setupSignal);
const follower = await recorded.follow(after, subscriber, signal);
// follower.head is the replay boundary. follower.done always resolves after
// cleanup; follower.error reports a failure. follower.close() is idempotent.
```

The consumer supplies `target` and each `subscriber` as the carrier the
composition may end. Closing a selected Wire closes its selected carrier;
closing a mount leaves its borrowed children usable. Recorder closure ends its
target and all followers. A follower's cancellation, storage-read failure,
target refusal or overflowing live handoff ends only that follower's carrier.

## The head and the bound

The recorder has one append worker. `Send` validates the path and admits work
without calling storage or a destination on the sender's stack. A successful
send promises admission, not append completion, delivery or an application
effect. `Head(ctx)` / `head(signal?)` fences earlier admitted appends. Setup
reads the store's initial head under the supplied setup context or optional
TypeScript signal; cancellation ends that read without taking ownership of
the target. The setup lifetime does not become the recorder's lifetime.

Taking a follower's head and registering its live handoff are one command in
the append worker's order. The follower reads `(after, head]` on its own worker,
then drains the handoff. Every later append enters that handoff exactly once.
No replay read holds the append worker. A store must support concurrent reads
and append; a durable store must not implement a replay-length append lock.

`MaxQueuedMessages` / `maxQueuedMessages` is the one queue bound, default 64.
It bounds waiting writer admissions, including head/attach commands, and each
follower's waiting live messages. The append worker also has its one active
operation. Overflow refuses admission and ends that endpoint with code 1008.
Storage and target failures end it with 1011. Ending and `OnClose` / `onClose`
run outside the sender's stack and append exclusion. Storage operations honor
cancellation so ending a recorder or follower also ends its worker.

Go uses the context passed to `Follow` for that follower's lifetime.
TypeScript accepts an optional `AbortSignal`. Cancelling a `Head` wait does not
cancel already admitted messages. A store operation must not wait for a later
command on its own recorder: that would wait for itself to finish.

## Consumer storage

| Responsibility | Go `WireLog` | TypeScript `WireLog` |
| --- | --- | --- |
| initial head | `Head(context.Context) (uint64, error)` | `head(signal): Promise<number>` |
| append and assign sequence | `Append(context.Context, []string, Message) (uint64, error)` | `append(path, message, signal): Promise<number>` |
| read one committed record | `Read(context.Context, uint64) (WireRecord, error)` | `read(sequence, signal): Promise<WireRecord>` |

A log has exactly one recorder writing it. Sequences are contiguous positive
integers through 2^53−1; zero is the empty head and the cursor before the first
message. A cursor ahead of the current head is refused. A missing or wrongly
numbered record fails replay rather than silently skipping a message. The
composition checks append and read sequence results; persistence, retention,
and translating storage failures are the consumer's.

`WireRecord` has `Sequence`, `Path`, `Message` in Go and `sequence`, `path`,
`message` in TypeScript. Messages are immutable after admission and when read
back. The memory log copies routing paths and preserves opaque messages,
including the identity of the local return capability. It does not serialize
that capability or any private context associated with it.

## Scope and declared history

An `after` cursor sent to another endpoint belongs in an ordinary declared
consumer request. It never appears in `channel.open`. This composition adds
no frame kind, correlation mechanism or reconnect protocol.

Replay preserves a live descriptor's original scope and owner lifetime. It
does not retain a binding, revive a released owner, translate a descriptor
to another connection, or make a native callable durable. Existing import and
release checks still apply. A consumer persisting opaque data must distinguish
that data from local capabilities that its storage cannot preserve.

The shared [head cases](../../conformance/tables/recorded-wire.json) drive
paired runtime and generated tests.

## Typed family events

Each generated binding and client package exposes a `RecordedEvent` union,
`Recorder`, and `Record` / `record` constructor. The binding union contains
server-declared outgoing events; the client union contains client-declared
outgoing events, even when the two sides use the same event name.

For a server-declared `changed` event carrying `Payload`:

```go
recorded, err := binding.Record(ctx, target, log, options, environment)
if err != nil { return err }
err = recorded.Append(ctx, binding.RecordedChanged{Data: payload})
```

```ts
const recorded = await binding.record(target, log, options, context);
await recorded.append({ name: 'changed', data: payload });
```

Go's sealed union has one `Recorded<Operation>` struct with a native `Data`
field per event. TypeScript's union discriminates native payloads by `name`;
a side with no outgoing events has `never`. Generic constructors take the
same positional bindings as that side's existing Wire adapters. Append uses
the existing event validation, conversion and publication path once, then
records the resulting opaque message. Live payloads require the existing
explicit owner in the call context.

Setup checks the closed declaration identity before reading the log's initial
head. Go's setup context, or TypeScript's final `WireCallOptions` argument,
cancels setup. Failure detaches the interpretation and leaves its borrowed
target usable. Typed `Follow` checks the subscriber's identity before admitting
replay; a mismatch touches neither its handlers nor the history. Identity
requests are never included in the event log. Head, bounds, storage, follower
failure and close have the raw composition's contract above.

Recording a live event stores its converted descriptor; it does not store the
native function as a fresh export. Replaying within the original scope uses
the original binding, and releasing that binding's owner invalidates aliases
and later imports. A descriptor interpreted on an unrelated scope cannot
invoke its original binding: the ordinary invocation refuses
`reference_unknown`. Raw descriptor bytes do not prove their scope at import.
This is preservation of an existing reference, not transfer to a new scope.

The shared [generated scenario](../../conformance/scenarios/generated/record-follow.json)
holds typed generic data history across local, mounted and forwarded Wires,
sockets and prepared channels, with both Go and TypeScript in both roles.
