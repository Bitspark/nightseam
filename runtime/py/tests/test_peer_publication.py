import asyncio
import unittest

from nightseam.duplex import pipe
from nightseam.runtime import Options, Peer, PublicError
from nightseam.runtime.publication import UnpublishedError


class PeerPublicationTests(unittest.IsolatedAsyncioTestCase):
    async def peers(self, options=None):
        a, b = pipe()
        client, server = Peer(a, options=options), Peer(b, "server")
        self.addAsyncCleanup(server.close)
        self.addAsyncCleanup(client.close)
        return client, server

    async def test_unadmitted_task_cancellation_retains_both_cancellation_and_proof(self):
        sending, release = asyncio.Event(), asyncio.Event()

        class BlockedConnection:
            async def send(self, frame):
                sending.set()
                await release.wait()

            async def receive(self):
                await asyncio.Future()

            async def close(self, code=1000, reason=""):
                release.set()

            def abort(self):
                release.set()

        peer = Peer(BlockedConnection(), options=Options(queue_capacity=1))
        self.addAsyncCleanup(peer.close)
        await peer.emit("active")
        await asyncio.wait_for(sending.wait(), 1)
        await peer.emit("queued")
        call = asyncio.create_task(peer.call("never_admitted"))
        await asyncio.sleep(0)
        self.assertEqual(len(peer._pending), 1)
        call.cancel()
        with self.assertRaises(asyncio.CancelledError) as cancelled:
            await call
        self.assertIsInstance(cancelled.exception, UnpublishedError)
        self.assertTrue(call.cancelled())
        self.assertEqual(peer.status, "connected")

    async def test_encoding_and_pending_refusals_prove_only_the_unsent_attempt(self):
        client, server = await self.peers(Options(max_pending_requests=1, max_frame_bytes=512))
        entered, release = asyncio.Event(), asyncio.Event()

        async def wait(value, context):
            entered.set()
            await release.wait()
            return value

        server.handle("wait", wait)
        for operation in (
            lambda: client.call("wait", object()),
            lambda: client.call("wait", "x" * 1024),
            lambda: client.emit("event", object()),
            lambda: client.emit("event", "x" * 1024),
        ):
            with self.assertRaises(UnpublishedError):
                await operation()
        first = asyncio.create_task(client.call("wait", 1))
        await asyncio.wait_for(entered.wait(), 1)
        try:
            with self.assertRaises(UnpublishedError) as error:
                await client.call("wait", 2)
            self.assertEqual(error.exception.code, "busy")
        finally:
            release.set()
        self.assertEqual(await first, 1)

    async def test_delivered_refusal_and_nested_unsent_failure_have_no_proof(self):
        client, server = await self.peers()

        def busy(value, context):
            raise PublicError("busy", "application already entered")

        async def nested(value, context):
            await server.call("unsent", object())

        server.handle("busy", busy)
        server.handle("nested", nested)
        for method in ("busy", "nested"):
            with self.assertRaises(PublicError) as error:
                await client.call(method)
            self.assertNotIsInstance(error.exception, UnpublishedError)

    async def test_queue_failure_does_not_give_existing_calls_an_unpublished_proof(self):
        sent = asyncio.Event()
        release = asyncio.Event()

        class FailingConnection:
            async def send(self, frame):
                sent.set()
                await release.wait()
                raise UnpublishedError(PublicError("busy", "nested adapter refusal"))

            async def receive(self):
                await asyncio.Future()

            async def close(self, code=1000, reason=""):
                pass

            def abort(self):
                pass

        peer = Peer(FailingConnection())
        self.addAsyncCleanup(peer.close)
        call = asyncio.create_task(peer.call("published"))
        await asyncio.wait_for(sent.wait(), 1)
        release.set()
        with self.assertRaises(PublicError) as error:
            await call
        self.assertNotIsInstance(error.exception, UnpublishedError)


if __name__ == "__main__":
    unittest.main()
