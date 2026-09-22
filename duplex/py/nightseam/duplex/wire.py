"""Relative-path bitwire.Wire contracts and pure selection and mount views."""

from __future__ import annotations

from collections.abc import Callable, Mapping
from typing import Literal

import bitwire

__all__ = [
    "WireError",
    "encode_path",
    "decode_path",
    "at",
    "mount",
]

class WireError(Exception):
    def __init__(self, code: Literal["closed", "no_route", "receiver_exists", "invalid_path"]):
        self.code = code
        super().__init__(f"bitwire.Wire {code}.")


def _utf8(value: str) -> bytes:
    try:
        return value.encode("utf-8")
    except UnicodeEncodeError:
        raise WireError("invalid_path") from None


def encode_path(path: bitwire.Path) -> str:
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
    def __init__(self, root: bitwire.Wire, path: bitwire.Path):
        self._root, self._prefix = root, list(path)

    def send(self, path: bitwire.Path, message: bitwire.Message) -> None:
        self._root.send([*self._prefix, *path], message)


def at(root: bitwire.Wire, path: bitwire.Path) -> bitwire.Wire:
    """Select send access; receiving and closure remain with the endpoint owner."""
    encode_path(path)
    return _Selected(root, path)


class _Mounted:
    def __init__(self, children: Mapping[str, bitwire.Endpoint]):
        self._routes = dict(children)
        self._attachment = None
        self._closed = False

    def send(self, path: bitwire.Path, message: bitwire.Message) -> None:
        if self._closed:
            raise WireError("closed")
        encode_path(path)
        if not path or path[0] not in self._routes:
            raise WireError("no_route")
        self._routes[path[0]].send(path[1:], message)

    def receive(self, receiver: bitwire.Receiver) -> Callable[[], None]:
        if self._closed:
            raise WireError("closed")
        if self._attachment is not None:
            raise WireError("receiver_exists")
        detaches = []
        attachment = object()
        self._attachment = attachment, receiver, detaches
        remaining = set(self._routes)

        def detach():
            if self._attachment is None or self._attachment[0] is not attachment:
                return
            self._attachment = None
            for stop in detaches:
                stop()
            detaches.clear()

        def ended(key, code, reason):
            remaining.discard(key)
            if not remaining and self._attachment is not None and self._attachment[0] is attachment:
                detach()
                if receiver.closed:
                    receiver.closed(code, reason)

        try:
            for key, child in self._routes.items():
                def delivered(path, message, key=key):
                    if receiver.message:
                        return receiver.message([key, *path], message)
                stop = child.receive(bitwire.Receiver(message=delivered,
                    closed=lambda code, reason, key=key: ended(key, code, reason)))
                if self._attachment is None:
                    stop()
                    raise WireError("closed")
                detaches.append(stop)
        except BaseException:
            detach()
            raise
        return detach

    def close(self, code: int = 1000, reason: str = "") -> None:
        if self._closed:
            return
        self._closed = True
        held, self._attachment = self._attachment, None
        if held is not None:
            _, receiver, detaches = held
            for detach in detaches:
                detach()
            if receiver.closed:
                receiver.closed(code, reason)


def mount(children: Mapping[str, bitwire.Endpoint]) -> bitwire.Endpoint:
    """Mount borrowed endpoints; closing releases only this mount's attachments."""
    return _Mounted(children)
