import asyncio
import unittest

from bitwire import Message, Receiver, ReturnAddress
from nightseam.duplex import Frame, at, encode_path, mount, pipe
from nightseam.duplex.websocket import dial, listen
from nightseam.runtime import Options, Peer, PublicError
from nightseam.runtime.json import loads
from routing import dispatcher


class PeerWireTests(unittest.IsolatedAsyncioTestCase):
    async def peers(self, options=None):
        left, right = pipe()
        client, server = Peer(left), Peer(right, "server", options)
        self.addAsyncCleanup(server.close)
        self.addAsyncCleanup(client.close)
        return client, server

    async def test_distinct_return_addresses_are_distinct_capabilities(self):
        carrier = object()
        first, second = ReturnAddress(carrier), ReturnAddress(carrier)
        self.assertIsNot(first, second)
        self.assertNotEqual(first, second)
        self.assertEqual(len({first, second}), 2)

    async def test_wire_is_a_stable_view_of_the_existing_peer(self):
        client, _ = await self.peers()
        self.assertIs(client.wire(), client.wire())
        self.assertIs(at(client.wire(), [])._root, client.wire())

    async def test_full_physical_handoff_is_refused_without_waiting_for_the_writer(self):
        for kind in ("event", "request"):
            with self.subTest(kind=kind):
                started, release = asyncio.Event(), asyncio.Event()

                class Blocked:
                    async def send(self, frame):
                        started.set()
                        await release.wait()

                    async def receive(self):
                        await asyncio.Future()

                    async def close(self, code=1000, reason=""):
                        pass

                    def abort(self):
                        pass

                peer = Peer(Blocked(), options=Options(queue_capacity=1, write_timeout_ms=10_000))
                self.addAsyncCleanup(peer.close)
                await peer.emit("active")
                await asyncio.wait_for(started.wait(), 1)
                await peer.emit("queued")
                replies = asyncio.Queue()

                class Returning:
                    def send(self, path, message):
                        replies.put_nowait(message.frame)

                frame = {"version": 1, "kind": kind}
                address = None
                if kind == "request":
                    frame.update(id="c:1", params={})
                    address = ReturnAddress(Returning())
                else:
                    frame["data"] = None
                peer.wire().send(["refused"], Message(frame, address))
                try:
                    # The ten-second transport deadline must not pace this
                    # synchronous-admission handoff. The root owns the refusal.
                    await asyncio.wait_for(peer._closed.wait(), 0.5)
                    self.assertEqual((await peer.wait_closed())["code"], 4011)
                    self.assertFalse(release.is_set())
                    if kind == "request":
                        self.assertEqual((await asyncio.wait_for(replies.get(), 1))["error"]["code"], "disconnected")
                finally:
                    release.set()

    async def test_paths_and_grouped_facets_use_the_existing_socket(self):
        from nightseam.runtime.wire import WireHandlers, call_wire, emit_wire, register_wire

        listener = await listen()
        self.addAsyncCleanup(listener.close)
        connection = await dial(listener.url)
        client, server = Peer(connection), Peer(await listener.accept(), "server")
        self.addAsyncCleanup(server.close)
        self.addAsyncCleanup(client.close)
        events = asyncio.Queue()
        paths = [[""], ["a/b"], ["a", "b"], ["😀", "\ufeff"]]
        for index, path in enumerate(paths):
            register_wire(
                dispatcher(server.wire()),
                path,
                WireHandlers(
                    request=lambda value, context, i=index: [i, value],
                    event=lambda value, context, i=index: events.put_nowait([i, value]),
                ),
            )
        view = at(mount({"service": client.wire()}), ["service"])
        for index, path in enumerate(paths):
            self.assertEqual(await call_wire(view, path, index), [index, index])
            emit_wire(view, path, index)
            self.assertEqual(await asyncio.wait_for(events.get(), 1), [index, index])

    async def test_request_event_and_cancel_admissions_keep_fifo(self):
        from nightseam.runtime.wire import emit_wire

        near, far = pipe()
        peer = Peer(near)
        self.addAsyncCleanup(peer.close)
        self.addCleanup(far.abort)
        replies = asyncio.Queue()

        class Return:
            def send(self, path, message):
                replies.put_nowait(message.frame)

        address = ReturnAddress(Return())
        wire = peer.wire()
        wire.send(["run"], Message({"version": 1, "kind": "request", "id": "c:1", "params": {}}, address))
        emit_wire(wire, ["before"])
        wire.send(["run"], Message({"version": 1, "kind": "cancel", "id": "c:1"}, address))
        emit_wire(wire, ["after"])
        frames = [loads((await asyncio.wait_for(far.receive(), 1)).data) for _ in range(4)]
        self.assertEqual([frame["kind"] for frame in frames], ["request", "event", "cancel", "event"])
        self.assertEqual(frames[0]["method"], encode_path(["run"]))
        self.assertEqual(frames[1]["event"], encode_path(["before"]))
        self.assertEqual((await asyncio.wait_for(replies.get(), 1))["error"]["code"], "cancelled")

    async def test_preparation_installs_receivers_before_any_read(self):
        from nightseam.runtime.wire import handle_wire

        near, far = pipe()
        self.addCleanup(near.abort)
        await near.send(Frame("text", '{"version":1,"kind":"request","id":"c:1","method":"4:echo","params":7}'))
        prepared = []

        def prepare(peer):
            prepared.append(peer.status)
            handle_wire(dispatcher(peer.wire()), ["echo"], lambda value, context: value)

        server = Peer(far, "server", Options(prepare=prepare))
        self.addAsyncCleanup(server.close)
        reply = loads((await asyncio.wait_for(near.receive(), 1)).data)
        self.assertEqual(prepared, ["connected"])
        self.assertEqual(reply["result"], 7)

    async def test_cancelled_wire_handler_retains_the_actual_execution_budget(self):
        from nightseam.runtime.wire import call_wire, handle_wire

        client, server = await self.peers(Options(max_concurrent_handlers=1, request_timeout_ms=45))
        entered, release = asyncio.Event(), asyncio.Event()
        count = 0

        async def work(value, context):
            nonlocal count
            count += 1
            entered.set()
            await release.wait()  # Deliberately ignore context cancellation.
            return value

        handle_wire(dispatcher(server.wire()), ["work"], work)
        first = asyncio.create_task(call_wire(client.wire(), ["work"], 1, timeout_ms=20))
        await asyncio.wait_for(entered.wait(), 1)
        try:
            with self.assertRaises(PublicError):
                await first
            await asyncio.sleep(0.06)  # The server's response deadline is past as well.
            with self.assertRaises(PublicError) as refused:
                await call_wire(client.wire(), ["work"], 2, timeout_ms=300)
            self.assertEqual(refused.exception.code, "busy")
            self.assertEqual(count, 1)
        finally:
            release.set()
        for _ in range(12):
            await asyncio.sleep(0)
        self.assertEqual(await call_wire(client.wire(), ["work"], 3), 3)

    async def test_cancellation_uses_captured_registration_after_detach(self):
        from nightseam.runtime.wire import call_wire, handle_wire

        client, server = await self.peers()
        entered, cancelled = asyncio.Event(), asyncio.Event()
        replacements = []

        async def original(value, context):
            entered.set()
            await context.cancelled.wait()
            cancelled.set()

        detach = handle_wire(dispatcher(server.wire()), ["hold"], original)
        call = asyncio.create_task(call_wire(client.wire(), ["hold"], timeout_ms=500))
        await asyncio.wait_for(entered.wait(), 1)
        detach()
        handle_wire(dispatcher(server.wire()), ["hold"], lambda value, context: replacements.append(value))
        call.cancel()
        with self.assertRaises(asyncio.CancelledError):
            await call
        await asyncio.wait_for(cancelled.wait(), 1)
        self.assertEqual(replacements, [])
        await call_wire(client.wire(), ["hold"], "new")
        self.assertEqual(replacements, ["new"])

    async def test_receiver_matching_is_exact_then_longest_namespace(self):
        from nightseam.runtime.wire import call_wire, response

        client, server = await self.peers()
        root = server.wire()
        dispatcher(root).register_prefix([], Receiver(message=lambda path, message: response(message, "root")))
        dispatcher(root).register_prefix(["a"], Receiver(message=lambda path, message: response(message, "a")))
        detach = dispatcher(root).register(
            ["a", "b"], Receiver(message=lambda path, message: response(message, "exact"))
        )
        self.assertEqual(await call_wire(client.wire(), ["a", "b"]), "exact")
        detach()
        self.assertEqual(await call_wire(client.wire(), ["a", "b"]), "a")
        self.assertEqual(await call_wire(client.wire(), ["x"]), "root")


if __name__ == "__main__":
    unittest.main()
