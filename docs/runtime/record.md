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
paired runtime tests. Generated family event unions and typed helpers are a
separate delivery under [#291](https://github.com/Bitspark/nightseam/issues/291);
the raw runtime composition does not claim that generator work is complete.
