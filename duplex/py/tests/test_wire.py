import unittest
from collections import deque

from nightseam.duplex.wire import Message, Receiver, ReturnAddress, WireError, at, decode_path, encode_path, mount


class QueuedRoot:
    """Controlled dispatch fixture; views must not invoke it on the sender's stack."""

    def __init__(self):
        self.queue = deque()
        self.receivers = {}
        self.closes = 0

    def send(self, path, message):
        if self.closes:
            raise WireError("closed")
        encode_path(path)
        self.queue.append((list(path), message))

    def receive(self, path, receiver):
        if self.closes:
            raise WireError("closed")
        key = (receiver.namespace, encode_path(path))
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
        if self.closes:
            return
        self.closes += 1
        receivers = list(self.receivers.values())
        self.receivers.clear()
        # Detachment from a close callback is permitted.
        for receiver in receivers:
            if receiver.closed:
                receiver.closed(code, reason)

    def drain(self):
        while self.queue:
            path, message = self.queue.popleft()
            encoded = encode_path(path)
            receiver = self.receivers.get((False, encoded))
            if receiver is None:
                candidates = [(len(prefix), item) for (namespace, prefix), item in self.receivers.items()
                              if namespace and encoded.startswith(prefix)]
                if candidates:
                    receiver = max(candidates, key=lambda item: item[0])[1]
            if receiver and receiver.message:
                receiver.message(path, message)


class WireTests(unittest.TestCase):
    def assert_wire_error(self, code, action):
        with self.assertRaises(WireError) as caught:
            action()
        self.assertEqual(caught.exception.code, code)

    def test_path_codec_is_injective_canonical_and_composes(self):
        paths = [[], [""], ["a", "b"], ["a.b"], ["a/b"], ["a", "b:c"],
                 ["é", "e\u0301", "😀", "\ufeff", "\0"]]
        encoded_paths = set()
        for path in paths:
            encoded = encode_path(path)
            self.assertNotIn(encoded, encoded_paths)
            encoded_paths.add(encoded)
            self.assertEqual(decode_path(encoded), path)
            for suffix in paths:
                self.assertEqual(encode_path(path + suffix), encoded + encode_path(suffix))
        self.assertEqual(encode_path(["a", "😀", ""]), "1:a4:😀0:")
        for malformed in ["01:a", "00:", "1", ":", "-1:a", "2:a", "1:é", "3:😀",
                          "99999999999999999999999999999:x", "1:\ud800", "1:\udc00"]:
            with self.subTest(encoded=ascii(malformed)):
                self.assert_wire_error("invalid_path", lambda: decode_path(malformed))
        for malformed in ["\ud800", "\udc00", "\ud83d\ude00"]:
            self.assert_wire_error("invalid_path", lambda: encode_path([malformed]))

    def test_views_preserve_frames_return_identity_and_async_dispatch(self):
        root, reply = QueuedRoot(), QueuedRoot()
        address = ReturnAddress(reply)
        prefix = ["a.b"]
        selected = at(root, prefix)
        prefix[0] = "changed"
        children = {"x": selected}
        mounted = mount(children)
        children["x"] = reply
        view = at(at(mounted, ["x"]), ["😀"])
        received = []
        detach = view.receive(["call"], Receiver(message=lambda path, message: received.append((path, message))))
        frames = [
            {"version": 1, "kind": "request", "id": "c:1", "params": {"n": 42}, "meta": {"tag": "value"}},
            {"version": 1, "kind": "response", "id": "c:1", "error": {"code": "refused", "message": "No"}},
            {"version": 1, "kind": "event", "data": None},
            {"version": 1, "kind": "cancel", "id": "c:1"},
        ]
        messages = [Message(frame, address) for frame in frames]
        for message in messages:
            view.send(["call"], message)
        self.assertEqual(received, [])
        self.assertEqual(len(root.queue), 4)
        self.assertEqual(len(reply.queue), 0)
        for path, _ in root.queue:
            self.assertEqual(path, ["a.b", "😀", "call"])
        root.drain()
        for (path, delivered), message in zip(received, messages, strict=True):
            self.assertEqual(path, ["call"])
            self.assertIs(delivered, message)
            self.assertIs(delivered.return_address, address)
        self.assert_wire_error("receiver_exists", lambda: at(root, ["a.b", "😀"]).receive(["call"], Receiver()))
        detach()
        detach()
        at(root, []).receive(["a.b", "😀", "call"], Receiver())()

    def test_namespace_restores_paths_and_keeps_healthy_child(self):
        left, right = QueuedRoot(), QueuedRoot()
        mounted = mount({"left": at(left, ["private"]), "": right})
        received, closed = [], []
        detach = mounted.receive([], Receiver(namespace=True,
            message=lambda path, message: received.append(path), closed=lambda *ending: closed.append(ending)))
        message = Message({"version": 1, "kind": "event", "data": None})
        mounted.send(["left", "nested", "call"], message)
        left.drain()
        left.close()
        self.assertEqual(closed, [])
        mounted.send(["", "still", "usable"], message)
        right.drain()
        self.assertEqual(received, [["left", "nested", "call"], ["", "still", "usable"]])
        detach()
        detach()
        self.assertEqual(right.receivers, {})
        self.assertEqual(right.closes, 0)
        mounted.close()
        self.assertEqual(closed, [])

    def test_namespace_closes_only_after_all_children_and_once(self):
        left, right = QueuedRoot(), QueuedRoot()
        mounted = mount({"left": left, "right": right})
        closed = []
        mounted.receive([], Receiver(namespace=True, closed=lambda *ending: closed.append(ending)))
        left.close(1000, "left")
        self.assertEqual(closed, [])
        right.close(1008, "right")
        self.assertEqual(closed, [(1008, "right")])
        mounted.close()
        self.assertEqual(len(closed), 1)

    def test_partial_namespace_acquisition_rolls_back(self):
        root = QueuedRoot()
        mounted = mount({"one": root, "two": root})
        self.assert_wire_error("receiver_exists", lambda: mounted.receive([], Receiver(namespace=True)))
        self.assertEqual(root.receivers, {})
        self.assertEqual(root.closes, 0)

    def test_detachment_retains_captured_request_cancellation(self):
        root = QueuedRoot()
        mounted = mount({"service": root})
        kinds = []
        detach = mounted.receive(["service", "wait"], Receiver(message=lambda path, message: kinds.append(message.frame["kind"])))
        accepted = root.receivers[(False, encode_path(["wait"]))]
        accepted.message(["wait"], Message({"version": 1, "kind": "request", "id": "c:1", "params": {}}))
        detach()
        accepted.message(["wait"], Message({"version": 1, "kind": "cancel", "id": "c:1"}))
        self.assertEqual(kinds, ["request", "cancel"])
        self.assertEqual(root.receivers, {})

    def test_mount_close_detaches_once_without_closing_children(self):
        root = QueuedRoot()
        mounted = mount({"": root})
        selected = at(mounted, [""])
        closed = []

        def closing(code, reason):
            closed.append((code, reason))
            mounted.close(code, reason)

        selected.receive(["call"], Receiver(closed=closing))
        self.assert_wire_error("no_route", lambda: mounted.receive([], Receiver()))
        self.assert_wire_error("no_route", lambda: mounted.send([], Message({"version": 1, "kind": "event", "data": None})))
        mounted.close(1000, "mount ended")
        mounted.close(1000, "again")
        self.assertEqual(closed, [(1000, "mount ended")])
        self.assertEqual(root.closes, 0)
        self.assertEqual(root.receivers, {})
        message = Message({"version": 1, "kind": "cancel", "id": "c:1"})
        self.assert_wire_error("closed", lambda: selected.send(["call"], message))
        self.assert_wire_error("closed", lambda: selected.receive(["call"], Receiver()))
        root.send(["call"], message)
        root.receive(["call"], Receiver())
        at(root, ["call"]).close()
        self.assertEqual(root.closes, 1)

    def test_mount_close_during_receive_leaves_no_child_registration(self):
        root = QueuedRoot()
        closed = []

        class Child:
            def receive(self, path, receiver):
                detach = root.receive(path, receiver)
                mounted.close(1000, "done")
                return detach

        mounted = mount({"x": Child()})
        self.assert_wire_error("closed", lambda: mounted.receive(["x", "call"], Receiver(closed=lambda *ending: closed.append(ending))))
        self.assertEqual(root.receivers, {})
        self.assertEqual(root.closes, 0)
        self.assertEqual(closed, [(1000, "done")])


if __name__ == "__main__":
    unittest.main()
