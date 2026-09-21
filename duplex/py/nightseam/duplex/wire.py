"""Relative-path Wire contracts and pure selection and mount views."""

from __future__ import annotations

from collections.abc import Awaitable, Callable, Mapping, Sequence
from dataclasses import dataclass
from typing import Literal, NotRequired, Protocol, TypedDict, runtime_checkable

__all__ = [
    "Path",
    "ProfileError",
    "ProfileFrame",
    "ReturnAddress",
    "Message",
    "Receiver",
    "Wire",
    "WireError",
    "encode_path",
    "decode_path",
    "at",
    "mount",
]

Path = Sequence[str]


class ProfileError(TypedDict):
    code: str
    message: str
    data: NotRequired[object]


class _TracedFrame(TypedDict):
    version: Literal[1]
    traceparent: NotRequired[str]
    tracestate: NotRequired[str]


class _RequestFrame(_TracedFrame):
    kind: Literal["request"]
    id: str
    params: object
    meta: NotRequired[Mapping[str, str]]


class _ResultFrame(_TracedFrame):
    kind: Literal["response"]
    id: str
    result: object


class _ErrorFrame(_TracedFrame):
    kind: Literal["response"]
    id: str
    error: ProfileError


class _EventFrame(_TracedFrame):
    kind: Literal["event"]
    data: object
    meta: NotRequired[Mapping[str, str]]


class _CancelFrame(_TracedFrame):
    kind: Literal["cancel"]
    id: str


# The send path supplies the method/event name; the frame has no second name.
ProfileFrame = _RequestFrame | _ResultFrame | _ErrorFrame | _EventFrame | _CancelFrame


@dataclass(frozen=True)
class ReturnAddress:
    """Local return capability preserved through views, never serialized."""

    wire: Wire


@dataclass(frozen=True)
class Message:
    frame: ProfileFrame
    return_address: ReturnAddress | None = None


@dataclass(frozen=True)
class Receiver:
    """Exact registration by default; namespace also captures descendants."""

    namespace: bool = False
    message: Callable[[Path, Message], None | Awaitable[None]] | None = None
    closed: Callable[[int, str], None] | None = None


@runtime_checkable
class Wire(Protocol):
    """Admission is synchronous; roots own bounded asynchronous dispatch.

    Receive refuses duplicates and returns an idempotent detach. Exact routes
    win over namespace routes, then the longest namespace prefix wins.
    """

    def send(self, path: Path, message: Message) -> None: ...
    def receive(self, path: Path, receiver: Receiver) -> Callable[[], None]: ...
    def close(self, code: int = 1000, reason: str = "") -> None: ...


class WireError(Exception):
    def __init__(self, code: Literal["closed", "no_route", "receiver_exists", "invalid_path"]):
        self.code = code
        super().__init__(f"Wire {code}.")


def _utf8(value: str) -> bytes:
    try:
        return value.encode("utf-8")
    except UnicodeEncodeError:
        raise WireError("invalid_path") from None


def encode_path(path: Path) -> str:
    """UTF-8 byte-length-prefixed scalar segments: [] is '', [''] is '0:'."""
    return "".join(f"{len(_utf8(segment))}:{segment}" for segment in path)


def decode_path(encoded: str) -> list[str]:
    """Accept canonical encoding only, preserving empty segments and BOMs."""
    data = _utf8(encoded)
    path = []
    offset = 0
    while offset < len(data):
        start, length = offset, 0
        while offset < len(data) and data[offset] != 58:
            digit = data[offset] - 48
            offset += 1
            if digit < 0 or digit > 9:
                raise WireError("invalid_path")
            length = length * 10 + digit
            if length > 9007199254740991:
                raise WireError("invalid_path")
        if offset == start or offset == len(data) or (offset - start > 1 and data[start] == 48):
            raise WireError("invalid_path")
        offset += 1
        if length > len(data) - offset:
            raise WireError("invalid_path")
        try:
            path.append(data[offset : offset + length].decode("utf-8"))
        except UnicodeDecodeError:
            raise WireError("invalid_path") from None
        offset += length
    return path


class _Selected:
    def __init__(self, root: Wire, path: Path):
        self._root = root
        self._prefix = list(path)

    def send(self, path: Path, message: Message) -> None:
        self._root.send([*self._prefix, *path], message)

    def receive(self, path: Path, receiver: Receiver) -> Callable[[], None]:
        def delivered(path: Path, message: Message):
            if receiver.message:
                return receiver.message(path[len(self._prefix) :], message)

        return self._root.receive(
            [*self._prefix, *path], Receiver(namespace=receiver.namespace, message=delivered, closed=receiver.closed)
        )

    def close(self, code: int = 1000, reason: str = "") -> None:
        self._root.close(code, reason)


def at(root: Wire, path: Path) -> Wire:
    """Select a copied path. Closing this view closes its existing endpoint."""
    return _Selected(root, path)


@dataclass(eq=False)
class _Registration:
    receiver: Receiver
    active: bool = True
    detach: Callable[[], None] | None = None


class _Mounted:
    def __init__(self, children: Mapping[str, Wire]):
        self._routes = dict(children)
        self._registrations: dict[_Registration, None] = {}
        self._closed = False

    def _destination(self, path: Path) -> Wire:
        if self._closed:
            raise WireError("closed")
        encode_path(path)
        if not path or path[0] not in self._routes:
            raise WireError("no_route")
        return self._routes[path[0]]

    def _remove(self, registration: _Registration, ending: tuple[int, str] | None = None) -> None:
        if not registration.active:
            return
        registration.active = False
        self._registrations.pop(registration, None)
        if registration.detach:
            registration.detach()
        if ending is not None and registration.receiver.closed:
            registration.receiver.closed(*ending)

    def send(self, path: Path, message: Message) -> None:
        self._destination(path).send(path[1:], message)

    def receive(self, path: Path, receiver: Receiver) -> Callable[[], None]:
        if not path and receiver.namespace:
            if self._closed:
                raise WireError("closed")
            keys = list(self._routes)
            detaches = []

            def detach_all():
                held = list(detaches)
                detaches.clear()
                for detach in held:
                    detach()

            registration = _Registration(receiver, detach=detach_all)
            self._registrations[registration] = None
            remaining = len(keys)

            def child_closed(code, reason):
                nonlocal remaining
                remaining -= 1
                if remaining == 0:
                    self._remove(registration, (code, reason))

            try:
                for key in keys:
                    detach = self.receive(
                        [key], Receiver(namespace=True, message=receiver.message, closed=child_closed)
                    )
                    if not registration.active:
                        detach()
                        raise WireError("closed")
                    detaches.append(detach)
            except BaseException:
                self._remove(registration)
                raise
            return lambda: self._remove(registration)

        child = self._destination(path)
        key = path[0]
        registration = _Registration(receiver)
        self._registrations[registration] = None

        def delivered(suffix: Path, message: Message):
            # Admitted requests retain this receiver for later cancellation.
            if receiver.message:
                return receiver.message([key, *suffix], message)

        try:
            registration.detach = child.receive(
                path[1:],
                Receiver(
                    namespace=receiver.namespace,
                    message=delivered,
                    closed=lambda code, reason: self._remove(registration, (code, reason)),
                ),
            )
        except BaseException:
            registration.active = False
            self._registrations.pop(registration, None)
            raise
        if not registration.active:
            registration.detach()
            raise WireError("closed")
        return lambda: self._remove(registration)

    def close(self, code: int = 1000, reason: str = "") -> None:
        if self._closed:
            return
        self._closed = True
        held = list(self._registrations)
        self._registrations.clear()
        for registration in held:
            registration.active = False
        for registration in held:
            if registration.detach:
                registration.detach()
        for registration in held:
            if registration.receiver.closed:
                registration.receiver.closed(code, reason)


def mount(children: Mapping[str, Wire]) -> Wire:
    """Consume one segment through a copied map; [] has no destination.

    Closing detaches this mount's registrations and leaves borrowed children
    usable. An empty string is a valid child key.
    """
    return _Mounted(children)
