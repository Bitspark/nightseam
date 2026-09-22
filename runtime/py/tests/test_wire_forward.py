import asyncio
import unittest

from bitwire import Message, ReturnAddress
from nightseam.runtime import PublicError
from nightseam.runtime.publication import UnpublishedError
from nightseam.runtime.wire import forward_wire
from test_wire_helpers import StubWire, request


class WireForwardTests(unittest.IsolatedAsyncioTestCase):
    async def test_forward_preserves_message_identity_and_borrows_both_endpoints(self):
        left, right = StubWire(), StubWire()
        detach = forward_wire(left, right)
        self.assertEqual(left.registrations[0][0], [])
        message = request(ReturnAddress(StubWire()))
        left.registrations[0][1].message(["outer", "leaf"], message)
        self.assertIs(right.sent[0][1], message)
        self.assertEqual(right.sent[0][0], ["outer", "leaf"])
        detach()
        detach()
        self.assertEqual((left.removed, right.removed), (1, 1))
        self.assertEqual((left.closed, right.closed), ([], []))
        # Captured callbacks still route cancellation after detach.
        cancel = Message({"version": 1, "kind": "cancel", "id": "c:1"}, message.return_address)
        left.registrations[0][1].message(["outer", "leaf"], cancel)
        self.assertIs(right.sent[-1][1], cancel)

    async def test_partial_registration_failure_detaches_first_side(self):
        left, right = StubWire(), StubWire()

        def refuse(receiver):
            raise PublicError("busy", "registration refused")

        right.receive = refuse
        with self.assertRaises(PublicError):
            forward_wire(left, right)
        self.assertEqual(left.removed, 1)
        self.assertEqual(left.closed, [])

    async def test_forward_failure_answers_without_lending_local_publication_proof(self):
        left, right, returning = StubWire(), StubWire(), StubWire()

        def refuse(path, message):
            raise UnpublishedError(PublicError("busy", "destination full"))

        right.on_send = refuse
        forward_wire(left, right)
        left.registrations[0][1].message(["operation"], request(ReturnAddress(returning)))
        error = returning.sent[0][1].frame["error"]
        self.assertEqual(error, {"code": "busy", "message": "destination full"})
        self.assertEqual((left.removed, right.removed), (1, 1))
        await asyncio.sleep(0)
