import asyncio
import unittest

import bitwire
from nightseam.duplex import WireError, at
from nightseam.runtime import Dispatcher, call_wire, wire_pair


class BitwireTests(unittest.IsolatedAsyncioTestCase):
    async def test_external_contract_receiver_answers_a_nightseam_call(self):
        left, right = wire_pair()
        self.addCleanup(left.close)
        self.assertIsInstance(left, bitwire.Endpoint)

        def answer(path, message):
            self.assertEqual(path, ["echo"])
            message.return_address.wire.send([], bitwire.Message({
                "version": 1, "kind": "response", "id": message.frame["id"],
                "result": message.frame["params"],
            }))

        detach = right.receive(bitwire.Receiver(message=answer))
        with self.assertRaises(WireError):
            right.receive(bitwire.Receiver())
        self.assertEqual(await call_wire(left, ["echo"], {"value": None}), {"value": None})
        detach()
        replacement = right.receive(bitwire.Receiver(message=answer))
        detach()
        self.assertEqual(await call_wire(left, ["echo"], 42), 42)
        replacement()

    async def test_dispatcher_composes_an_external_endpoint_without_unwrapping(self):
        class External:
            def __init__(self):
                self.receiver = None
                self.closed = False

            def send(self, path, message):
                receiver = self.receiver
                asyncio.get_running_loop().call_soon(receiver.message, path, message)

            def receive(self, receiver):
                if self.receiver is not None:
                    raise RuntimeError("already attached")
                self.receiver = receiver

                def detach():
                    if self.receiver is receiver:
                        self.receiver = None
                return detach

            def close(self, code=1000, reason=""):
                self.closed = True

        endpoint = External()
        router = Dispatcher(endpoint)
        self.addCleanup(router.close)
        view = router.select(["model"])
        received = asyncio.Queue()
        view.receive(bitwire.Receiver(message=lambda path, message: received.put_nowait((path, message))))
        message = bitwire.Message({"version": 1, "kind": "event", "data": None})
        wire = at(endpoint, ["model"])
        self.assertIsInstance(wire, bitwire.Wire)
        self.assertNotIsInstance(wire, bitwire.Endpoint)
        wire.send(["changed"], message)
        path, delivered = await asyncio.wait_for(received.get(), 1)
        self.assertEqual(path, ["changed"])
        self.assertIs(delivered, message)
        router.close()
        self.assertFalse(endpoint.closed)
        self.assertIsNone(endpoint.receiver)
