import unittest
from collections import deque

from bitwire import Message, Receiver, ReturnAddress
from nightseam.duplex.wire import WireError, at, decode_path, encode_path, mount


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

    def receive(self, receiver):
        if self.closes:
            raise WireError("closed")
        if self.receivers:
            raise WireError("receiver_exists")
        self.receivers[None] = receiver

        def detach():
            if self.receivers.get(None) is receiver:
                del self.receivers[None]

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
            receiver = self.receivers.get(None)
            if receiver and receiver.message:
                receiver.message(path, message)


class WireTests(unittest.TestCase):
    def assert_wire_error(self, code, action):
        with self.assertRaises(WireError) as caught:
            action()
        self.assertEqual(caught.exception.code, code)

    def test_path_codec_is_injective_canonical_and_composes(self):
        paths = [[], [""], ["a", "b"], ["a.b"], ["a/b"], ["a", "b:c"], ["é", "e\u0301", "😀", "\ufeff", "\0"]]
        encoded_paths = set()
        for path in paths:
            encoded = encode_path(path)
            self.assertNotIn(encoded, encoded_paths)
            encoded_paths.add(encoded)
            self.assertEqual(decode_path(encoded), path)
            for suffix in paths:
                self.assertEqual(encode_path(path + suffix), encoded + encode_path(suffix))
        self.assertEqual(encode_path(["a", "😀", ""]), "1:a4:😀0:")
        for malformed in [
            "01:a",
            "00:",
            "1",
            ":",
            "-1:a",
            "2:a",
            "1:é",
            "3:😀",
            "99999999999999999999999999999:x",
            "1:\ud800",
            "1:\udc00",
        ]:
            with self.subTest(encoded=ascii(malformed)):
                self.assert_wire_error("invalid_path", lambda: decode_path(malformed))
        for malformed in ["\ud800", "\udc00", "\ud83d\ude00"]:
            self.assert_wire_error("invalid_path", lambda: encode_path([malformed]))

    def test_views_preserve_frames_return_identity_and_async_dispatch(self):
        from nightseam.runtime import Dispatcher

        root, reply = QueuedRoot(), QueuedRoot()
        address = ReturnAddress(reply)
        router = Dispatcher(root)
        prefix = ["a.b"]
        selected = router.select(prefix)
        prefix[0] = "changed"
        mounted = mount({"x": selected})
        views = Dispatcher(mounted)
        view = views.select(["x", "😀"])
        received = []
        detach = view.receive(Receiver(message=lambda path, message: received.append((path, message))))
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
        self.assert_wire_error("receiver_exists", lambda: view.receive(Receiver()))
        detach()
        detach()
        view.receive(Receiver())()
        views.close()
        router.close()
        self.assertEqual(root.closes, 0)

    def test_mount_restores_paths_and_keeps_healthy_child(self):
        left, right = QueuedRoot(), QueuedRoot()
        mounted = mount({"left": left, "": right})
        received, closed = [], []
        detach = mounted.receive(
            Receiver(message=lambda path, message: received.append(path), closed=lambda *ending: closed.append(ending))
        )
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

    def test_mount_closes_only_after_all_children_and_once(self):
        left, right = QueuedRoot(), QueuedRoot()
        mounted = mount({"left": left, "right": right})
        closed = []
        mounted.receive(Receiver(closed=lambda *ending: closed.append(ending)))
        left.close(1000, "left")
        self.assertEqual(closed, [])
        right.close(1008, "right")
        self.assertEqual(closed, [(1008, "right")])
        mounted.close()
        self.assertEqual(len(closed), 1)

    def test_partial_acquisition_rolls_back(self):
        root = QueuedRoot()
        mounted = mount({"one": root, "two": root})
        self.assert_wire_error("receiver_exists", lambda: mounted.receive(Receiver()))
        self.assertEqual(root.receivers, {})
        self.assertEqual(root.closes, 0)

    def test_detachment_preserves_admitted_callback_and_stale_detach(self):
        root = QueuedRoot()
        mounted = mount({"service": root})
        kinds = []
        detach = mounted.receive(Receiver(message=lambda path, message: kinds.append(message.frame["kind"])))
        accepted = root.receivers[None]
        accepted.message(["wait"], Message({"version": 1, "kind": "request", "id": "c:1", "params": {}}))
        detach()
        new = mounted.receive(Receiver())
        detach()
        self.assertTrue(root.receivers)
        accepted.message(["wait"], Message({"version": 1, "kind": "cancel", "id": "c:1"}))
        self.assertEqual(kinds, ["request", "cancel"])
        new()
        self.assertEqual(root.receivers, {})

    def test_selection_only_grants_send_and_mount_borrows_children(self):
        from bitwire import Endpoint, Wire

        root = QueuedRoot()
        mounted = mount({"": root})
        selected = at(mounted, [""])
        self.assertIsInstance(selected, Wire)
        self.assertNotIsInstance(selected, Endpoint)
        self.assertFalse(hasattr(selected, "close"))
        closed = []
        mounted.receive(Receiver(closed=lambda *ending: closed.append(ending)))
        self.assert_wire_error("receiver_exists", lambda: mounted.receive(Receiver()))
        self.assert_wire_error(
            "no_route", lambda: mounted.send([], Message({"version": 1, "kind": "event", "data": None}))
        )
        mounted.close(1000, "mount ended")
        mounted.close(1000, "again")
        self.assertEqual(closed, [(1000, "mount ended")])
        self.assertEqual(root.closes, 0)
        self.assertEqual(root.receivers, {})
        root.receive(Receiver())()

    def test_mount_close_during_receive_leaves_no_child_attachment(self):
        root = QueuedRoot()
        closed = []

        class Child:
            def receive(self, receiver):
                detach = root.receive(receiver)
                mounted.close(1000, "done")
                return detach

        mounted = mount({"x": Child()})
        self.assert_wire_error(
            "closed", lambda: mounted.receive(Receiver(closed=lambda *ending: closed.append(ending)))
        )
        self.assertEqual(root.receivers, {})
        self.assertEqual(root.closes, 0)
        self.assertEqual(closed, [(1000, "done")])
