"""Acceptance-only consumer composition; record/follow is not a runtime API."""

import asyncio
from collections import deque
from dataclasses import dataclass

from bitwire import Message, Path, Receiver, Wire
from nightseam.duplex.wire import WireError, at, mount
from nightseam.runtime import Dispatcher


class _Queue:
    def __init__(self):
        self.items = deque()
        self.waiters = deque()

    def put(self, value):
        while self.waiters:
            waiter = self.waiters.popleft()
            if not waiter.done():
                waiter.set_result(value)
                return
        self.items.append(value)

    async def take(self):
        if self.items:
            return self.items.popleft()
        waiter = asyncio.get_running_loop().create_future()
        self.waiters.append(waiter)
        try:
            return await waiter
        finally:
            if waiter in self.waiters:
                self.waiters.remove(waiter)


@dataclass(frozen=True)
class _Entry:
    path: Path
    message: Message
    sequence: int


class _Follower:
    def __init__(self):
        self.live = _Queue()
        self.sent = _Queue()
        self.paused = asyncio.Event()
        self.resume = asyncio.Event()
        self.stop = asyncio.Event()
        self.stopped = False
        self.done = None

    def end(self):
        self.stopped = True
        self.stop.set()

    async def until_stopped(self, work):
        operation = asyncio.create_task(work)
        stopping = asyncio.create_task(self.stop.wait())
        try:
            await asyncio.wait((operation, stopping), return_when=asyncio.FIRST_COMPLETED)
            if self.stopped:
                return None
            return operation.result()
        finally:
            for task in (operation, stopping):
                if not task.done():
                    task.cancel()
            await asyncio.gather(operation, stopping, return_exceptions=True)


class _RecordedWire:
    def __init__(self):
        self.entries = []
        self.followers = {}
        self.appending = False

    def head(self):
        if self.appending:
            raise AssertionError("application callback ran under append exclusion")
        return len(self.entries)

    def send(self, path: Path, message: Message):
        self.appending = True
        try:
            entry = _Entry(list(path), message, len(self.entries) + 1)
            self.entries.append(entry)
            for follower, bound in list(self.followers.items()):
                if len(follower.live.items) == bound:
                    del self.followers[follower]
                    # Signal only: the subscriber writer closes its own carrier
                    # outside append and off the producer's stack.
                    follower.end()
                else:
                    follower.live.put(entry)
        finally:
            self.appending = False

    def receive(self, receiver):
        raise WireError("no_route")

    def close(self, code=1000, reason=""):
        for follower in self.followers:
            follower.end()
        self.followers.clear()

    def attach(self, after: int, target: Wire, bound: int, pause: bool):
        follower = _Follower()
        # One synchronous critical section: append cannot enter between the
        # head snapshot and registration of its bounded live handoff.
        self.appending = True
        try:
            head = len(self.entries)
            history = self.entries[after:head]
            self.followers[follower] = bound
        finally:
            self.appending = False

        def send(entry):
            if follower.stopped:
                return False
            target.send(entry.path, entry.message)
            follower.sent.put(entry.sequence)
            return True

        async def write():
            try:
                for index, entry in enumerate(history):
                    if not send(entry):
                        return
                    if index == 0 and pause:
                        follower.paused.set()
                        await follower.until_stopped(follower.resume.wait())
                        if follower.stopped:
                            return
                while not follower.stopped:
                    entry = await follower.until_stopped(follower.live.take())
                    if entry is None or not send(entry):
                        return
            finally:
                target.close(1008, "recorded handoff ended")

        follower.done = asyncio.create_task(write())
        return head, follower


class _RecordedRoot:
    """Private root with queued asynchronous delivery, without transport APIs."""

    def __init__(self):
        self.receivers = {}
        self.queue = deque()
        self.scheduled = False
        self.closed = False

    def send(self, path: Path, message: Message):
        if self.closed:
            raise WireError("closed")
        if len(self.queue) == 16:
            raise AssertionError("witness output queue full")
        self.queue.append((list(path), message))
        if self.scheduled:
            return
        self.scheduled = True
        asyncio.get_running_loop().call_soon(self._drain)

    def _drain(self):
        self.scheduled = False
        while not self.closed and self.queue:
            path, message = self.queue.popleft()
            receiver = self.receivers.get(None)
            if receiver and receiver.message:
                receiver.message(list(path), message)

    def receive(self, receiver: Receiver):
        key = None
        if key in self.receivers:
            raise WireError("receiver_exists")
        self.receivers[key] = receiver
        active = True

        def detach():
            nonlocal active
            if active:
                active = False
                self.receivers.pop(key, None)

        return detach

    def close(self, code=1000, reason=""):
        self.closed = True
        self.queue.clear()
        self.receivers.clear()


def _message(value):
    return Message({"version": 1, "kind": "event", "data": value})


class _Presentation:
    def __init__(self, store):
        self.root = _RecordedRoot()
        self.end = _RecordedRoot()
        self.values = _Queue()
        self.closed = _Queue()
        self.failure = None
        self.root_routes = Dispatcher(self.root)
        self.end_routes = Dispatcher(self.end)
        self.destination_routes = Dispatcher(mount({"out": self.end_routes.select(["destination"])}))
        destination = self.destination_routes.select(["out"])
        self.source_routes = Dispatcher(mount({"outer": mount({"in": self.root_routes.select(["source"])})}))
        self.wire = self.source_routes.select(["outer", "in"])

        def deliver(path, message):
            try:
                if path != ["tick"]:
                    raise AssertionError("recorded destination received wrong path")
                store.head()  # Application reentry must be outside append exclusion.
                if message.frame["kind"] != "event" or type(message.frame["data"]) is not int:
                    raise AssertionError("expected event")
                self.values.put(message.frame["data"])
            except Exception as error:
                self.failure = error
                self.values.put(0)

        destination.receive(Receiver(message=deliver))

        def forward(path, message):
            try:
                destination.send(path, message)
            except Exception as error:
                self.failure = error
                self.values.put(0)

        def closed(code, reason):
            store.head()
            self.closed.put(code)

        self.wire.receive(Receiver(message=forward, closed=closed))

    async def collect(self):
        # A fence on the same route follows all earlier replay/live deliveries.
        self.wire.send(["tick"], _message(0))
        values = []
        while True:
            value = await self.values.take()
            if self.failure:
                raise self.failure
            if value == 0:
                return values
            values.append(value)

    def close(self):
        self.wire.close()
        self.source_routes.close()
        self.destination_routes.close()
        self.root_routes.close()
        self.end_routes.close()
        self.root.close()
        self.end.close()


async def _sent(follower, last):
    while await follower.sent.take() != last:
        pass  # Wait for the subscriber writer's admission barrier.


async def _head_case(before):
    store = _RecordedWire()
    first, late = _Presentation(store), _Presentation(store)
    followers = []
    try:
        source = at(mount({"record": store}), ["record"])
        for value in range(1, 4):
            source.send(["tick"], _message(value))
        if before:
            source.send(["tick"], _message(4))
        head, follower = store.attach(0, first.wire, 2, True)
        followers.append(follower)
        await follower.paused.wait()

        async def produce():
            for value in range(5 if before else 4, 6):
                source.send(["tick"], _message(value))

        # Producer completion is observed while replay remains at its barrier.
        await asyncio.create_task(produce())
        follower.resume.set()
        await _sent(follower, 5)
        source.send(["tick"], _message(6))
        await _sent(follower, 6)
        values = await first.collect()
        _, second = store.attach(3, late.wire, 2, False)
        followers.append(second)
        await _sent(second, 6)
        return {
            "cut": "append_before_head" if before else "head_before_append",
            "head": head,
            "first": values,
            "after_three": await late.collect(),
            "producer_progress": True,
            "callbacks_outside_append": True,
        }
    finally:
        store.close()
        try:
            await asyncio.gather(*(follower.done for follower in followers))
        finally:
            first.close()
            late.close()


async def _stall_case():
    store = _RecordedWire()
    stalled, healthy = _Presentation(store), _Presentation(store)
    followers = []
    try:
        for value in range(1, 4):
            store.send(["tick"], _message(value))
        _, slow = store.attach(0, stalled.wire, 2, True)
        followers.append(slow)
        await slow.paused.wait()
        _, fast = store.attach(3, healthy.wire, 2, False)
        followers.append(fast)
        for value in range(4, 6):
            store.send(["tick"], _message(value))
            await _sent(fast, value)
        queued = len(slow.live.items)
        store.send(["tick"], _message(6))
        await _sent(fast, 6)
        await asyncio.shield(slow.done)
        code = await stalled.closed.take()
        try:
            stalled.wire.send(["tick"], _message(99))
        except WireError as error:
            if error.code != "closed":
                raise
        else:
            raise AssertionError("stalled carrier accepted after close")
        underneath = _Queue()

        def probe_received(path, message):
            if message.frame["kind"] == "event":
                underneath.put(message.frame["data"])

        stalled.root_routes.register(["probe"], Receiver(message=probe_received))
        stalled.root.send(["probe"], _message(99))
        probe = await underneath.take()
        store.send(["tick"], _message(7))
        await _sent(fast, 7)
        return {
            "bound": 2,
            "queued_at_bound": queued,
            "closed": 1 + len(stalled.closed.items),
            "close_code": code,
            "healthy": await healthy.collect(),
            "underneath": [probe],
            "head": store.head(),
            "producer_progress": True,
        }
    finally:
        store.close()
        try:
            await asyncio.gather(*(follower.done for follower in followers))
        finally:
            stalled.close()
            healthy.close()


async def recorded_wire_witness(within_ms: int):
    """Exercise both head cuts and overflow isolation within one deadline."""
    async with asyncio.timeout(within_ms / 1000):
        return {"cases": [await _head_case(False), await _head_case(True)], "stalled": await _stall_case()}
