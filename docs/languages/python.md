# Python

Python requires **3.11 or later** and provides the core frame connection,
asyncio peer, descriptor validator, identity exchange, bounded relative-path
Wire endpoints and recording/following over consumer storage. Its current support
is tier 4; the [language table](../../README.md#languages)
records the conformance results. The [onboarding order](onboarding.md) names the
later generation, tunnel, live and observability work.

## Installing from this checkout

From the repository root, install the source into your active Python environment:

```sh
python -m pip install .
```

Or build a wheel and install the resulting file into a consumer environment:

```sh
python -m pip install 'build>=1.2,<2'
python -m build --wheel
python -m pip install /path/to/nightseam-VERSION-py3-none-any.whl
```

Replace the last path with the wheel produced under `dist/`. These commands
install the local distribution; they do not depend on a Nightseam package
being available from a public registry. The wheel includes `nightseam.duplex`
and `nightseam.runtime`, and installs its WebSocket transport dependency.

## A peer over an in-memory connection

```python
import asyncio

from nightseam.duplex import pipe
from nightseam.runtime import Peer


async def main():
    left, right = pipe()
    server = Peer(right, "server")
    server.handle("echo", lambda value, context: value)
    client = Peer(left)
    try:
        assert await client.call("echo", {"text": "hello"}) == {"text": "hello"}
    finally:
        await client.close()
        await server.close()


asyncio.run(main())
```

Register handlers before yielding control to the event loop. `Peer` takes any
`FrameConnection`; `nightseam.duplex.websocket.dial` and `listen` provide real
socket connections. Request handlers receive `(value, context)`; named event
listeners registered with `on_event` receive the same pair. A handler can be
synchronous or async. `PublicError` is the handler exception whose code,
message and optional data reach the caller. `context.cancelled` is an
`asyncio.Event`, which cooperative handlers can await or inspect.

An omitted call payload is `{}` and an omitted event payload is `null`;
Python `None` is explicit JSON null. `ABSENT` keeps absence distinct from
null, including optional public-error data and incoming metadata. `RawJSON`
retains a payload's original JSON spelling when forwarding it.

## One Wire over local and physical access

`wire_pair(options=None)` creates bounded local endpoints without constructing
a Peer or a transport. `peer.wire()` returns the same root over an existing
peer. Both implement the [relative Wire contract](../runtime/wire.md): sends
admit synchronously, requests run concurrently, and events retain their order.

```python
from nightseam.runtime import call_wire, handle_wire, wire_pair


async def local_echo():
    access, binding = wire_pair()
    detach = handle_wire(binding, ["echo"], lambda value, context: value)
    try:
        return await call_wire(access, ["echo"], {"text": "hello"})
    finally:
        detach()
        access.close()
```

`register_wire(wire, path, WireHandlers(request=..., event=...))` registers both
facets together. `on_wire_event` and synchronous `emit_wire` provide the event
helpers. `forward_wire(left, right)` connects two relative origins and returns
an idempotent detach; detach leaves both borrowed endpoints usable. `at` and
`mount` compose these origins without creating more carriers or queues.

Request contexts expose `wire`, `request_id`, `cancelled`, `trace` and `meta`;
event contexts expose `wire`, `trace` and `meta`. Received private context
survives local composition and ends at a physical hop. Supplying `context=`
to an outgoing helper propagates trace context; copy metadata only by passing
`meta=context.meta` explicitly. Cancelling a call ends the caller's wait promptly;
the incoming response waits for the handler to exit unless its receiver deadline
expires first. A cancelled handler retains its active-work slot until it actually
exits, even when its response deadline has passed.

For early incoming traffic, `Options(prepare=callback)` invokes `callback(peer)`
synchronously before the reader and writer start. Install Wire receivers there;
begin asynchronous traffic after Peer construction returns.

`UnpublishedError` identifies a particular call or emit refused before queue
admission. It permits rollback of that unsent attempt. A received refusal or a
carrier failure after admission carries no such proof. Cancellation before
admission retains both this marker and native `asyncio.CancelledError` behavior.

## Recording and following

`record(target, log, options=None)` composes a Wire with consumer-owned storage.
The returned `RecordedWire` admits each message synchronously, appends it on a
worker, and then forwards it to `target`. `await recorded.head()` fences earlier
admitted appends. It does not promise delivery or an application effect.

```python
import asyncio

from nightseam.duplex import MemoryWireLog, Message, RecordOptions, record


async def follow_until_stopped(target, subscriber, stopped):
    closed = asyncio.Event()
    recorded = await record(
        target,
        MemoryWireLog(),
        RecordOptions(max_queued_messages=64, on_close=lambda error: closed.set()),
    )
    try:
        recorded.send(["changed"], Message({"version": 1, "kind": "event", "data": 1}))
        async with await recorded.follow(0, subscriber) as follower:
            assert follower.head == 1
            await stopped.wait()
    finally:
        recorded.close()
        await closed.wait()
```

The host supplies the two Wire carriers and an `asyncio.Event` named `stopped`.
The follower takes its head and registers its live handoff in append order,
replays `(after, head]`, then follows later appends exactly once. Its replay
worker does not hold up appends. The queue bound covers waiting recorder
commands, including head/attach commands, and each follower's live handoff;
the append worker can also have one active operation.

`WireLog` is the storage protocol: async `head()`, `append(path, message)` and
`read(sequence)`. Storage has one exclusive recorder writer, supports reads
concurrent with append, and must honor task cancellation. Sequences are
contiguous positive integers through `2**53 - 1`; zero denotes an empty head
or the cursor before the first message. `WireRecord` has immutable `sequence`,
`path` and `message` fields. The memory log copies paths and preserves opaque
messages and their return capabilities; callers must not mutate messages after
admission or after reading them. It does not retain a binding, revive a released
owner, or translate a capability to a new reference scope.

Setup cancellation leaves `target` usable. Cancelling a pending head wait does
not cancel admitted appends. A cancelled attachment is either skipped or cleaned
up before its caller receives cancellation. A follower's async context manager
closes and joins its worker when its body exits, including on cancellation.
Manual ownership uses idempotent `follower.close()` followed by
`await follower.wait_closed()`. Cancelling that wait leaves cleanup running.
`follower.error` retains a failure or context cancellation; `wait_closed()`
itself completes normally after cleanup.

Recorder closure ends its target and followers. A replay failure, target refusal
or overflowing handoff ends only that follower's target. Overflow uses close
code 1008; storage or target failure uses 1011. `RecordError.code` is `sequence`
or `overflow`; consumer failures preserve their original exception. Carrier
closure and `on_close` run off the sender's stack, and `on_close` runs once after
the recorder and follower workers finish and their carrier close calls return.
Supplying a mount as a target keeps
its borrowed children usable after closure. The [shared recording
contract](../runtime/record.md) describes storage and scope ownership; cursors
sent remotely remain ordinary consumer request data.

## Checking a change

```sh
python -m pip install -e '.[test]'
python -m unittest discover -s duplex/py/tests
python -m unittest discover -s runtime/py/tests
python -m unittest discover -s conformance/python -p 'test_*.py'
python scripts/smoke-python.py
```

The smoke builds a temporary source copy, installs its wheel into a new virtual
environment outside the checkout, and holds imports and a real WebSocket
request through mounted, forwarded local Wire endpoints. It also checks the
wheel's modules, version and notices.
CI, nightly conformance and release checks install Python and run these gates.
The shared Go conformance runner exercises Python on either side of the socket;
an unavailable Python interpreter or dependency fails its setup.
