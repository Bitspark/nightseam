import asyncio
import unittest

from nightseam.duplex import CloseError, Frame, pipe
from nightseam.runtime import ABSENT, Options, Peer, PublicError
from nightseam.runtime.json import loads


class GatedConnection:
    def __init__(self):
        self.sending = asyncio.Event()
        self.release = asyncio.Event()
        self.closed = asyncio.Event()
        self.incoming = asyncio.Queue()
        self.sent = []

    async def send(self, frame):
        self.sending.set()
        await self.release.wait()
        self.sent.append(loads(frame.data))

    async def receive(self):
        return await self.incoming.get()

    async def close(self, code=1000, reason=""):
        self.closed.set()

    def abort(self):
        self.closed.set()


class PeerTests(unittest.IsolatedAsyncioTestCase):
    async def test_unadmitted_call_cancellation_never_waits_for_or_uses_output_capacity(self):
        for timeout in (True, False):
            with self.subTest(timeout=timeout):
                connection = GatedConnection()
                peer = Peer(connection, options=Options(queue_capacity=1, write_timeout_ms=1000))
                self.addAsyncCleanup(peer.close)
                await peer.emit("first")
                await connection.sending.wait()
                await peer.emit("second")
                call = asyncio.create_task(peer.call("unadmitted", timeout_ms=20 if timeout else 1000))
                await asyncio.sleep(0)
                if not timeout:
                    call.cancel()
                expected = PublicError if timeout else asyncio.CancelledError
                with self.assertRaises(expected):
                    await asyncio.wait_for(call, 0.3)
                self.assertEqual(peer.status, "connected")
                connection.release.set()
                for _ in range(10):
                    await asyncio.sleep(0)
                self.assertEqual([frame["kind"] for frame in connection.sent], ["event", "event"])

    async def test_admitted_call_cancellation_is_best_effort_when_output_is_full(self):
        connection = GatedConnection()
        peer = Peer(connection, options=Options(queue_capacity=1, write_timeout_ms=1000))
        self.addAsyncCleanup(peer.close)
        call = asyncio.create_task(peer.call("admitted"))
        await connection.sending.wait()
        await peer.emit("queued")
        call.cancel()
        with self.assertRaises(asyncio.CancelledError):
            await asyncio.wait_for(call, 0.3)
        self.assertEqual(peer.status, "connected")
        connection.release.set()
        for _ in range(10):
            await asyncio.sleep(0)
        self.assertEqual([frame["kind"] for frame in connection.sent], ["request", "event"])

    async def test_close_finishes_even_when_application_suppresses_task_cancellation(self):
        a, b = pipe()
        peer = Peer(b, "server", Options(write_timeout_ms=40))
        entered, release = asyncio.Event(), asyncio.Event()

        async def ignore(value, context):
            entered.set()
            while not release.is_set():
                try:
                    await release.wait()
                except asyncio.CancelledError:
                    pass

        peer.handle("ignore", ignore)
        await a.send(Frame("text", '{"version":1,"kind":"request","id":"c:1","method":"ignore","params":{}}'))
        await entered.wait()
        closing = asyncio.create_task(peer.close())
        try:
            await asyncio.wait_for(asyncio.shield(closing), 0.3)
            with self.assertRaises(CloseError):
                await a.receive()
            self.assertEqual((await peer.wait_closed())["code"], 1000)
        finally:
            release.set()
            await asyncio.wait_for(closing, 1)

    async def test_overload_refusal_cannot_park_the_reader_behind_output_capacity(self):
        connection = GatedConnection()
        peer = Peer(connection, "server", Options(queue_capacity=1, max_concurrent_handlers=1, write_timeout_ms=1000))
        self.addAsyncCleanup(peer.close)
        entered = asyncio.Event()

        async def hold(value, context):
            entered.set()
            await context.cancelled.wait()

        peer.handle("hold", hold)
        connection.incoming.put_nowait(
            Frame("text", '{"version":1,"kind":"request","id":"c:1","method":"hold","params":{}}')
        )
        await entered.wait()
        call = asyncio.create_task(peer.call("reverse"))
        await connection.sending.wait()
        await peer.emit("queued")
        connection.incoming.put_nowait(
            Frame("text", '{"version":1,"kind":"request","id":"c:2","method":"hold","params":{}}')
        )
        connection.incoming.put_nowait(Frame("text", '{"version":1,"kind":"response","id":"s:1","result":7}'))
        # Admission may end an overloaded carrier, but must never wait for its
        # writer before processing or settling an already pending response.
        done, _ = await asyncio.wait([call], timeout=0.3)
        try:
            self.assertIn(call, done)
            try:
                self.assertEqual(await call, 7)
            except PublicError as error:
                self.assertEqual(error.code, "disconnected")
        finally:
            connection.release.set()
            if not call.done():
                call.cancel()
                await asyncio.gather(call, return_exceptions=True)

    async def pair(self, **options):
        a, b = pipe()
        client = Peer(a, "client", Options(**options))
        server = Peer(b, "server", Options(**options))
        self.addAsyncCleanup(client.close)
        self.addAsyncCleanup(server.close)
        return client, server

    async def test_calls_preserve_presence_and_reverse_calls(self):
        client, server = await self.pair()
        server.handle("echo", lambda value, context: value)
        client.handle("back", lambda value, context: {"back": value})
        server.handle("reverse", lambda value, context: server.call("back", value, context=context))
        self.assertEqual(await client.call("echo"), {})
        for value in (None, False, 0, "", [], {}):
            self.assertEqual(await client.call("echo", value), value)
        self.assertEqual(await client.call("reverse", "x"), {"back": "x"})

    async def test_cancel_signals_without_ending_work_that_ignores_it(self):
        client, server = await self.pair()
        entered, released = asyncio.Event(), asyncio.Event()
        contexts = []

        async def hold(value, context):
            contexts.append(context)
            entered.set()
            await released.wait()
            return "late result"

        server.handle("hold", hold)
        server.handle("echo", lambda value, context: value)
        call = asyncio.create_task(client.call("hold"))
        await asyncio.wait_for(entered.wait(), 1)
        call.cancel()
        with self.assertRaises(asyncio.CancelledError):
            await call
        await asyncio.wait_for(contexts[0].cancelled.wait(), 1)
        self.assertEqual(await client.call("echo", "still usable"), "still usable")
        self.assertEqual(len(server._incoming), 1)
        released.set()
        await asyncio.sleep(0.01)
        self.assertEqual(len(server._incoming), 0)

    async def test_deadline_and_public_error_are_distinct(self):
        client, server = await self.pair()

        async def wait(value, context):
            await context.cancelled.wait()

        server.handle("wait", wait)
        with self.assertRaises(PublicError) as error:
            await client.call("wait", timeout_ms=20)
        self.assertEqual(error.exception.code, "request_timeout")

        def refuse(value, context):
            raise PublicError("cancelled", "application decision", None)

        server.handle("refuse", refuse)
        with self.assertRaises(PublicError) as error:
            await client.call("refuse")
        self.assertEqual(error.exception.data, None)

    async def test_busy_refuses_before_starting_another_request(self):
        client, server = await self.pair(max_pending_requests=1)
        entered = asyncio.Event()

        async def wait(value, context):
            entered.set()
            await context.cancelled.wait()

        server.handle("wait", wait)
        first = asyncio.create_task(client.call("wait"))
        await asyncio.wait_for(entered.wait(), 1)
        with self.assertRaises(PublicError) as error:
            await client.call("wait")
        self.assertEqual(error.exception.code, "busy")
        first.cancel()
        with self.assertRaises(asyncio.CancelledError):
            await first

    async def test_trace_propagates_and_metadata_does_not(self):
        client, server = await self.pair()
        contexts = []

        def back(value, context):
            contexts.append(context)
            return value

        client.handle("back", back)

        async def forward(value, context):
            contexts.append(context)
            return await server.call("back", value, context=context)

        server.handle("forward", forward)
        await client.call("forward", {}, meta={"tenant": "one", "nightseam.reserved": "drop"})
        self.assertEqual(contexts[0].meta, {"tenant": "one"})
        self.assertIs(contexts[1].meta, ABSENT)
        outer, inner = [context.trace["traceparent"].split("-") for context in contexts]
        self.assertEqual(outer[1], inner[1])
        self.assertNotEqual(outer[2], inner[2])

    async def test_raw_payload_is_forwarded_without_reencoding(self):
        raw, connection = pipe()
        server = Peer(connection, "server")
        self.addAsyncCleanup(server.close)
        server.handle("echo", lambda value, context: context.raw)
        await raw.send(Frame("text", '{"version":1,"kind":"request","id":"c:1","method":"echo","params":1e3}'))
        response = await raw.receive()
        self.assertIn('"result":1e3', response.data)

    async def test_bad_handler_output_settles_the_call_and_peer_stays_usable(self):
        client, server = await self.pair()
        server.handle("bad", lambda value, context: "\ud800")
        server.handle("echo", lambda value, context: value)
        with self.assertRaises(PublicError) as error:
            await client.call("bad")
        self.assertEqual(error.exception.code, "internal")
        self.assertEqual(await client.call("echo", "okay"), "okay")

    async def test_event_order_and_stalled_handler_end_the_connection(self):
        client, server = await self.pair(write_timeout_ms=40)
        values = []
        server.on_event("item", lambda value, context: values.append(value))
        for index in range(4):
            await client.emit("item", index)
        server.handle("echo", lambda value, context: value)
        await client.call("echo")
        await asyncio.sleep(0.01)
        self.assertEqual(values, list(range(4)))

        async def stuck(value, context):
            await asyncio.Event().wait()

        server.on_event("stuck", stuck)
        await client.emit("stuck")
        closed = await asyncio.wait_for(server.wait_closed(), 1)
        self.assertFalse(closed["clean"])


if __name__ == "__main__":
    unittest.main()
