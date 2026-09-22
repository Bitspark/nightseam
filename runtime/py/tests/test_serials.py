import asyncio
import json
import unittest
from pathlib import Path

from nightseam.duplex import Frame, pipe
from nightseam.runtime import Options, Peer, PublicError
from test_peer import GatedConnection


class SerialTests(unittest.IsolatedAsyncioTestCase):
    async def test_shared_serial_table(self):
        table = json.loads((Path(__file__).parents[3] / "conformance/tables/serials.json").read_text(encoding="utf8"))
        for row in table["rows"]:
            with self.subTest(row=row["name"]):
                raw, carrier = pipe()
                peer = Peer(carrier, row["to"])
                peer.handle("echo", lambda value, context: value)
                try:
                    await raw.send(Frame("text", row["before"]))
                    # Consume a response to prove completed IDs still cannot be reused.
                    if json.loads(row["before"])["kind"] == "request":
                        await asyncio.wait_for(raw.receive(), 1)
                    await raw.send(Frame("text", row["frame"]))
                    if row["valid"]:
                        self.assertEqual(
                            json.loads((await asyncio.wait_for(raw.receive(), 1)).data)["kind"], "response"
                        )
                        self.assertEqual(peer.status, "connected")
                    else:
                        self.assertEqual((await asyncio.wait_for(peer.wait_closed(), 1))["code"], 4011)
                finally:
                    await peer.close()

    async def test_reservation_and_publication_share_one_gate(self):
        reserved, release = asyncio.Event(), asyncio.Event()

        class PausedPeer(Peer):
            async def _send(self, envelope, *args, **kwargs):
                if envelope.get("id") == "c:1":
                    reserved.set()
                    await release.wait()
                await super()._send(envelope, *args, **kwargs)

        conn = GatedConnection()
        conn.release.set()
        peer = PausedPeer(conn, options=Options(queue_capacity=1))
        calls = [asyncio.create_task(peer.call("first"))]
        try:
            await asyncio.wait_for(reserved.wait(), 1)
            calls.append(asyncio.create_task(peer.call("second")))
            for _ in range(10):
                await asyncio.sleep(0)
            release.set()
            async with asyncio.timeout(1):
                while len(conn.sent) < 2:
                    await asyncio.sleep(0)
            self.assertEqual([f["id"] for f in conn.sent], ["c:1", "c:2"])
        finally:
            release.set()
            for call in calls:
                call.cancel()
            await asyncio.gather(*calls, return_exceptions=True)
            await peer.close()

    async def test_failed_encoding_spends_serial_and_exhaustion_closes(self):
        conn = GatedConnection()
        conn.release.set()
        peer = Peer(conn)
        try:
            with self.assertRaises(PublicError):
                await peer.call("bad", object())
            call = asyncio.create_task(peer.call("next"))
            await asyncio.wait_for(conn.sending.wait(), 1)
            self.assertEqual(conn.sent[0]["id"], "c:2")
            call.cancel()
            await asyncio.gather(call, return_exceptions=True)
            peer._next = 9007199254740991
            with self.assertRaises(PublicError) as error:
                await peer.call("overflow")
            self.assertEqual(error.exception.code, "identifier_exhausted")
            self.assertEqual(peer.status, "disconnected")
        finally:
            await peer.close()
