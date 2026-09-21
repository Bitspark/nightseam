"""Ordered, bounded frames connections; no protocol or application policy."""

import asyncio
from collections import deque
from dataclasses import dataclass
from typing import Protocol, runtime_checkable

from .record import Follower, MemoryWireLog, RecordedWire, RecordError, RecordOptions, WireLog, WireRecord, record
from .wire import (
    Message,
    Path,
    ProfileError,
    ProfileFrame,
    Receiver,
    ReturnAddress,
    Wire,
    WireError,
    at,
    decode_path,
    encode_path,
    mount,
)

__all__ = [
    "CloseError",
    "Frame",
    "FrameConnection",
    "pipe",
    "Message",
    "Path",
    "ProfileError",
    "ProfileFrame",
    "Receiver",
    "ReturnAddress",
    "Wire",
    "WireError",
    "at",
    "decode_path",
    "encode_path",
    "mount",
    "Follower",
    "MemoryWireLog",
    "RecordedWire",
    "RecordError",
    "RecordOptions",
    "WireLog",
    "WireRecord",
    "record",
]


@dataclass(frozen=True)
class Frame:
    kind: str
    data: str | bytes

    def __post_init__(self):
        if self.kind not in ("text", "binary"):
            raise ValueError("duplex frame of no kind")
        if self.kind == "binary" and not isinstance(self.data, bytes):
            raise ValueError("binary frame requires bytes")
        if self.kind == "text" and not isinstance(self.data, (str, bytes)):
            raise ValueError("text frame requires a string or UTF-8 bytes")

    @property
    def size(self):
        return len(self.data.encode("utf8")) if isinstance(self.data, str) else len(self.data)


class CloseError(ConnectionError):
    def __init__(self, code=1006, reason=""):
        self.code, self.reason = code, reason
        super().__init__(f"duplex connection closed ({code})" + (": " + reason if reason else ""))


@runtime_checkable
class FrameConnection(Protocol):
    async def send(self, frame: Frame) -> None: ...
    async def receive(self) -> Frame: ...
    async def close(self, code: int = 1000, reason: str = "") -> None: ...
    def abort(self) -> None: ...


class _Pipe:
    def __init__(self, limit):
        self.limit = limit
        self.frames = deque()
        self.changed = asyncio.Event()
        self.ended = None
        self.remote = None
        self.subprotocol = ""

    async def send(self, frame):
        while True:
            if self.ended:
                raise CloseError(self.ended.code, self.ended.reason)
            if self.remote.ended:
                raise CloseError(self.remote.ended.code, self.remote.ended.reason)
            if len(self.frames) < 8:
                self.frames.append(frame)
                self.remote.changed.set()
                return
            self.changed.clear()
            await self.changed.wait()

    async def receive(self):
        while True:
            if self.ended:
                raise CloseError(self.ended.code, self.ended.reason)
            if self.remote.frames:
                frame = self.remote.frames.popleft()
                self.remote.changed.set()
                if self.limit > 0 and frame.size > self.limit:
                    self.abort()
                    raise CloseError(1009, "frame exceeds receive limit")
                return frame
            if self.remote.ended:
                raise CloseError(self.remote.ended.code, self.remote.ended.reason)
            self.changed.clear()
            await self.changed.wait()

    async def close(self, code=1000, reason=""):
        self._end(code, reason)

    def abort(self):
        self._end(1006, "")

    def _end(self, code, reason):
        if self.ended is None:
            self.ended = CloseError(code, reason)
            self.changed.set()
            self.remote.changed.set()


def pipe(limit=1 << 20):
    """Two ends with eight frames in flight per direction; a ninth send waits."""
    if limit < 0:
        raise ValueError("receive limit must not be negative")
    a, b = _Pipe(limit), _Pipe(limit)
    a.remote, b.remote = b, a
    return a, b
