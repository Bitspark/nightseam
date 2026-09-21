import asyncio
import unittest

from nightseam.duplex import CloseError, Frame, pipe
from nightseam.duplex.websocket import dial, listen


class ConnectionTests(unittest.IsolatedAsyncioTestCase):
    async def test_listener_close_releases_connections_waiting_for_accept_capacity(self):
        server = await listen()
        clients = []
        try:
            for _ in range(9):
                clients.append(await dial(server.url))
            await asyncio.wait_for(server.close(), 1)
        finally:
            for client in clients:
                client.abort()
            # Let a broken implementation clean up after the failed assertion.
            while not server._accepted.empty():
                server._accepted.get_nowait()
            await server.close()

    async def exercise(self, a, b):
        frames = [Frame("text", "hello"), Frame("text", "😀"), Frame("binary", bytes(range(256)))]
        for frame in frames:
            await a.send(frame)
            self.assertEqual(await b.receive(), frame)
            await b.send(frame)
            self.assertEqual(await a.receive(), frame)
        await a.send(Frame("text", "before close"))
        closing = asyncio.create_task(a.close(4007, "finished"))
        self.assertEqual(await b.receive(), Frame("text", "before close"))
        with self.assertRaises(CloseError) as error:
            await b.receive()
        self.assertEqual((error.exception.code, error.exception.reason), (4007, "finished"))
        await closing
        with self.assertRaises(CloseError):
            await b.send(Frame("text", "after close"))

    async def test_pipe_and_websocket_hold_the_same_seam(self):
        a, b = pipe()
        await self.exercise(a, b)
        server = await listen("127.0.0.1", 0)
        self.addAsyncCleanup(server.close)
        a = await dial(server.url)
        b = await server.accept()
        await self.exercise(a, b)

    async def test_pipe_is_bounded_and_cancellation_releases_the_send(self):
        a, b = pipe()
        self.addCleanup(a.abort)
        self.addCleanup(b.abort)
        for index in range(8):
            await a.send(Frame("text", str(index)))
        send = asyncio.create_task(a.send(Frame("text", "ninth")))
        await asyncio.sleep(0.01)
        self.assertFalse(send.done())
        send.cancel()
        with self.assertRaises(asyncio.CancelledError):
            await send
        for index in range(8):
            self.assertEqual((await b.receive()).data, str(index))
        await a.send(Frame("text", "next"))
        self.assertEqual((await b.receive()).data, "next")

    async def test_abort_releases_both_receivers(self):
        a, b = pipe()
        waiting = asyncio.create_task(b.receive())
        a.abort()
        with self.assertRaises(CloseError) as error:
            await asyncio.wait_for(waiting, 1)
        self.assertEqual(error.exception.code, 1006)

    async def test_receive_limit_is_utf8_bytes(self):
        a, b = pipe(limit=3)
        await a.send(Frame("text", "😀"))
        with self.assertRaises(CloseError):
            await b.receive()
        with self.assertRaises(CloseError):
            await a.send(Frame("text", "x"))


if __name__ == "__main__":
    unittest.main()
