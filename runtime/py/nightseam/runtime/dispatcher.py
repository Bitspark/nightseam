"""Explicit routing above one borrowed Bitwire endpoint attachment."""

import weakref
from dataclasses import dataclass

import bitwire
from nightseam.duplex import WireError, encode_path


@dataclass(eq=False)
class _Registration:
    path: tuple
    receiver: bitwire.Receiver


class Dispatcher:
    """Exact routes precede the longest prefix; closure releases the attachment.

    A dispatcher grants registration authority. Its selected views are Bitwire
    endpoints, so composition never needs to recognize this concrete class.
    """

    def __init__(self, root: bitwire.Endpoint, *, own_endpoint=False):
        self._root = root
        self._own_endpoint = own_endpoint
        self._routes = {}
        self._captured = weakref.WeakKeyDictionary()
        self._closed = False
        self._detach = None
        detach = root.receive(bitwire.Receiver(message=self._deliver, closed=self.close))
        if self._closed:
            detach()
            raise WireError("closed")
        self._detach = detach

    def send(self, path: bitwire.Path, message: bitwire.Message):
        if self._closed:
            raise WireError("closed")
        self._root.send(path, message)

    def register(self, path: bitwire.Path, receiver: bitwire.Receiver):
        return self._register(path, receiver, False)

    def register_prefix(self, path: bitwire.Path, receiver: bitwire.Receiver):
        return self._register(path, receiver, True)

    def _register(self, path, receiver, prefix):
        encode_path(path)
        key = tuple(path), prefix
        if self._closed:
            raise WireError("closed")
        if key in self._routes:
            raise WireError("receiver_exists")
        registration = _Registration(tuple(path), receiver)
        self._routes[key] = registration

        def detach():
            if self._routes.get(key) is registration:
                del self._routes[key]
        return detach

    def _deliver(self, path, message):
        address = message.return_address
        kind = message.frame["kind"]
        captured = self._captured.get(address, {}) if address is not None else {}
        if kind == "cancel":
            registration = captured.get(message.frame["id"])
        else:
            registration = self._routes.get((tuple(path), False))
            if registration is None:
                candidates = [r for (prefix, is_prefix), r in self._routes.items()
                              if is_prefix and tuple(path[:len(prefix)]) == prefix]
                registration = max(candidates, key=lambda r: len(r.path), default=None)
            if kind == "request" and address is not None and registration is not None:
                captured[message.frame["id"]] = registration
                self._captured[address] = captured
        if registration is not None and registration.receiver.message:
            return registration.receiver.message(path, message)
        if kind == "request":
            from .peer import PublicError
            from .wire import response
            response(message, error=PublicError("method_not_found", "No handler at this path"))
        return None

    def select(self, path: bitwire.Path) -> bitwire.Endpoint:
        encode_path(path)
        return _SelectedEndpoint(self, tuple(path))

    def close(self, code=1000, reason=""):
        if self._closed:
            return
        self._closed = True
        if self._detach:
            self._detach()
            self._detach = None
        held = list(self._routes.values())
        self._routes.clear()
        for registration in held:
            if registration.receiver.closed:
                registration.receiver.closed(code, reason)
        if self._own_endpoint:
            self._root.close(code, reason)


class _SelectedEndpoint:
    def __init__(self, dispatcher, prefix):
        self._dispatcher = dispatcher
        self._prefix = prefix
        self._closed = False
        self._receiver = None
        self._detach = None

    def send(self, path, message):
        if self._closed:
            raise WireError("closed")
        self._dispatcher.send([*self._prefix, *path], message)

    def receive(self, receiver):
        if self._closed:
            raise WireError("closed")
        if self._receiver is not None:
            raise WireError("receiver_exists")
        self._receiver = receiver

        def delivered(path, message):
            if receiver.message:
                return receiver.message(path[len(self._prefix):], message)

        def ended(code, reason):
            if self._receiver is receiver:
                self._receiver = None
                self._detach = None
                if receiver.closed:
                    receiver.closed(code, reason)

        try:
            stop = self._dispatcher.register_prefix(self._prefix,
                bitwire.Receiver(message=delivered, closed=ended))
        except BaseException:
            self._receiver = None
            raise

        def detach():
            if self._receiver is receiver:
                self._receiver = None
                self._detach = None
                stop()
        self._detach = detach
        return detach

    def close(self, code=1000, reason=""):
        if self._closed:
            return
        self._closed = True
        receiver = self._receiver
        if self._detach:
            self._detach()
        if receiver and receiver.closed:
            receiver.closed(code, reason)
