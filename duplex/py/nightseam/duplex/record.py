"""Bounded recording and independent following of opaque Wire messages."""

from __future__ import annotations

import asyncio
from collections import deque
from collections.abc import Callable
from dataclasses import dataclass
from typing import Literal, Protocol

from bitwire import Message, Path, Receiver, Wire

from .wire import WireError, encode_path

__all__ = [
    "WireRecord",
    "WireLog",
    "MemoryWireLog",
    "RecordError",
    "RecordOptions",
    "RecordedWire",
    "Follower",
    "record",
]

_MAX_SEQUENCE = 2**53 - 1


def _sequence(value):
    return type(value) is int and 0 <= value <= _MAX_SEQUENCE


class RecordError(Exception):
    def __init__(self, code: Literal["sequence", "overflow"]):
        self.code = code
        super().__init__(f"Record {code}.")


@dataclass(frozen=True)
class WireRecord:
    """Immutable routing and sequence; the message retains its original scope."""

    sequence: int
    path: tuple[str, ...]
    message: Message

    def __post_init__(self):
        object.__setattr__(self, "path", tuple(self.path))


class WireLog(Protocol):
    """Consumer storage with one exclusive recorder writing contiguous sequences.

    Reads may overlap append. Operations must honor asyncio cancellation.
    Persistence adds no serialization or lifetime rule for local capabilities.
    """

    async def head(self) -> int: ...
    async def append(self, path: Path, message: Message) -> int: ...
    async def read(self, sequence: int) -> WireRecord: ...


class MemoryWireLog:
    """Keep immutable messages and copied paths without serializing capabilities.

    This log is used on one event loop. Admitted and returned messages must not
    be mutated by their callers; no deep copy or new owner is introduced.
    """

    def __init__(self):
        self._entries: list[WireRecord] = []

    async def head(self) -> int:
        return len(self._entries)

    async def append(self, path: Path, message: Message) -> int:
        sequence = len(self._entries) + 1
        if not _sequence(sequence):
            raise RecordError("sequence")
        self._entries.append(WireRecord(sequence, tuple(path), message))
        return sequence

    async def read(self, sequence: int) -> WireRecord:
        if not _sequence(sequence) or sequence == 0 or sequence > len(self._entries):
            raise RecordError("sequence")
        return self._entries[sequence - 1]


@dataclass(frozen=True)
class RecordOptions:
    """One bound for waiting writer commands and each follower's live handoff."""

    max_queued_messages: int = 64
    on_close: Callable[[BaseException | None], None] | None = None


@dataclass(frozen=True)
class _Append:
    path: tuple[str, ...]
    message: Message


@dataclass(frozen=True)
class _Control:
    future: asyncio.Future
    action: Callable


class RecordedWire:
    """Append before forwarding; successful send promises admission only.

    Construct with ``await record(target, log)``. Closing owns the supplied
    target and followers. The setup task does not own the recorder's lifetime.
    """

    def __init__(self, target: Wire, log: WireLog, options: RecordOptions, head: int):
        self._target, self._log, self._options = target, log, options
        self._head = head
        self._loop = asyncio.get_running_loop()
        self._queue: deque[_Append | _Control] = deque()
        self._followers: set[Follower] = set()
        self._closed = False
        self._running = False
        self._task: asyncio.Task | None = None
        self._cleanup_task: asyncio.Task | None = None

    def _admit(self, command):
        if self._closed:
            raise WireError("closed")
        if len(self._queue) >= self._options.max_queued_messages:
            error = RecordError("overflow")
            self._end(1008, "record queue is full", error)
            raise error
        self._queue.append(command)
        if not self._running:
            self._running = True
            # create_task alone can run immediately under an eager task factory.
            self._loop.call_soon(self._start)

    def _start(self):
        if not self._closed:
            self._task = self._loop.create_task(self._run())

    def send(self, path: Path, message: Message) -> None:
        encode_path(path)
        self._admit(_Append(tuple(path), message))

    def receive(self, receiver: Receiver) -> Callable[[], None]:
        if self._closed:
            raise WireError("closed")
        return self._target.receive(receiver)

    def close(self, code: int = 1000, reason: str = "") -> None:
        self._end(code, reason, None)

    def _end(self, code, reason, error):
        if self._closed:
            return
        self._closed = True
        for command in self._queue:
            if isinstance(command, _Control) and not command.future.done():
                command.future.set_exception(WireError("closed"))
        self._queue.clear()
        if self._task and not self._task.done() and self._task is not asyncio.current_task():
            self._task.cancel()
        followers = tuple(self._followers)
        for follower in followers:
            follower._end(code, reason, error)
        self._loop.call_soon(self._start_cleanup, followers, code, reason, error)

    def _start_cleanup(self, followers, code, reason, error):
        self._cleanup_task = self._loop.create_task(self._cleanup(followers, code, reason, error))

    async def _cleanup(self, followers, code, reason, error):
        if self._task:
            await asyncio.gather(self._task, return_exceptions=True)
        await asyncio.gather(*(follower.wait_closed() for follower in followers))
        try:
            self._target.close(code, reason)
        except Exception as closing:
            self._loop.call_exception_handler({"message": "record carrier close failed", "exception": closing})
        if self._options.on_close:
            try:
                self._options.on_close(error)
            except Exception as callback:
                self._loop.call_exception_handler({"message": "record close observer failed", "exception": callback})

    async def _run(self):
        try:
            while self._queue and not self._closed:
                command = self._queue.popleft()
                if isinstance(command, _Control):
                    if not command.future.done():
                        try:
                            command.future.set_result(command.action(self._head))
                        except Exception as error:
                            command.future.set_exception(error)
                    continue
                sequence = await self._log.append(command.path, command.message)
                if self._closed:
                    return
                if not _sequence(sequence) or sequence != self._head + 1:
                    raise RecordError("sequence")
                self._head = sequence
                entry = WireRecord(sequence, command.path, command.message)
                for follower in tuple(self._followers):
                    if not follower._offer(entry):
                        follower._end(1008, "record handoff queue is full", RecordError("overflow"))
                self._target.send(list(entry.path), entry.message)
        except asyncio.CancelledError as error:
            if not self._closed:
                self._end(1011, "record storage cancelled", error)
        except Exception as error:
            self._end(1011, "record storage or target failed", error)
        finally:
            self._running = False

    def _control(self, action):
        future = self._loop.create_future()
        self._admit(_Control(future, action))
        return future

    async def head(self) -> int:
        """Fence earlier admitted appends without promising target delivery."""
        return await self._control(lambda head: head)

    async def follow(self, after: int, target: Wire) -> Follower:
        """Atomically attach at a head, replay (after, head], then follow appends.

        The returned follower owns its target. Use its async context manager to
        tie that lifetime to application task cancellation.
        """
        if target is None:
            raise ValueError("follow requires a target")

        def attach(head):
            if not _sequence(after) or after > head:
                raise RecordError("sequence")
            return Follower(self, target, after, head)

        future = self._control(attach)
        try:
            return await future
        except asyncio.CancelledError as error:
            # Cancellation may race with a committed attachment whose handle
            # has not reached its caller. That carrier must not be orphaned.
            if future.done() and not future.cancelled() and future.exception() is None:
                follower = future.result()
                follower._end(1000, "follow cancelled", error)
                await follower.wait_closed()
            raise


class Follower:
    """One replay writer and bounded handoff, owning only its supplied target."""

    def __init__(self, owner: RecordedWire, target: Wire, after: int, head: int):
        self._owner, self._target, self._head = owner, target, head
        self._loop = owner._loop
        self._live: deque[WireRecord] = deque()
        self._wake = asyncio.Event()
        self._stopped = False
        self._error: BaseException | None = None
        self._done = self._loop.create_future()
        self._task: asyncio.Task | None = None
        self._cleanup_task: asyncio.Task | None = None
        owner._followers.add(self)
        self._loop.call_soon(self._start, after)

    @property
    def head(self) -> int:
        return self._head

    @property
    def error(self) -> BaseException | None:
        return self._error

    def close(self) -> None:
        self._end(1000, "follow closed", None)

    async def wait_closed(self) -> None:
        """Wait for writer and carrier cleanup; failures are held in error."""
        await asyncio.shield(self._done)

    async def __aenter__(self) -> Follower:
        return self

    async def __aexit__(self, exception_type, exception, traceback) -> None:
        if isinstance(exception, asyncio.CancelledError):
            self._end(1000, "follow cancelled", exception)
        else:
            self.close()
        await self.wait_closed()

    def _start(self, after):
        if not self._stopped:
            self._task = self._loop.create_task(self._run(after))

    def _offer(self, entry):
        if self._stopped:
            return True
        if len(self._live) >= self._owner._options.max_queued_messages:
            return False
        self._live.append(entry)
        self._wake.set()
        return True

    def _end(self, code, reason, error):
        if self._stopped:
            return
        self._stopped = True
        self._error = error
        self._live.clear()
        self._wake.set()
        if self._task and not self._task.done() and self._task is not asyncio.current_task():
            self._task.cancel()
        self._loop.call_soon(self._start_cleanup, code, reason)

    def _start_cleanup(self, code, reason):
        self._cleanup_task = self._loop.create_task(self._cleanup(code, reason))

    async def _cleanup(self, code, reason):
        if self._task:
            await asyncio.gather(self._task, return_exceptions=True)
        try:
            self._target.close(code, reason)
        except Exception as closing:
            self._loop.call_exception_handler({"message": "follow carrier close failed", "exception": closing})
        finally:
            self._owner._followers.discard(self)
            self._done.set_result(None)

    def _send(self, entry):
        if not self._stopped:
            self._target.send(list(entry.path), entry.message)

    async def _run(self, after):
        try:
            for sequence in range(after + 1, self._head + 1):
                if self._stopped:
                    return
                entry = await self._owner._log.read(sequence)
                if self._stopped:
                    return
                if not _sequence(entry.sequence) or entry.sequence != sequence:
                    raise RecordError("sequence")
                self._send(entry)
            while not self._stopped:
                if self._live:
                    self._send(self._live.popleft())
                else:
                    self._wake.clear()
                    await self._wake.wait()
        except asyncio.CancelledError as error:
            if not self._stopped:
                self._end(1011, "record replay cancelled", error)
        except Exception as error:
            self._end(1011, "record replay or target failed", error)
        finally:
            self._end(1000, "follow ended", None)


async def record(target: Wire, log: WireLog, options: RecordOptions | None = None) -> RecordedWire:
    """Read the initial head, then take ownership of the target and log writer.

    Cancelling setup leaves the target usable. Later storage operations run on
    owned workers; storage must honor their task cancellation. Opaque messages
    retain their existing capabilities, scope and owner lifetime.
    """
    if target is None or log is None:
        raise ValueError("record requires a target and storage")
    options = options if options is not None else RecordOptions()
    if not _sequence(options.max_queued_messages) or options.max_queued_messages == 0:
        raise ValueError("record queue bound must be a positive safe integer")
    head = await log.head()
    if not _sequence(head):
        raise RecordError("sequence")
    return RecordedWire(target, log, options, head)
