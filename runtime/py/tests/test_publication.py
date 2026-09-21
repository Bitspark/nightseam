import asyncio
import unittest

from nightseam.runtime import PublicError, RequestContext
from nightseam.runtime.publication import UnpublishedError, unpublished, without_unpublished_proof
from nightseam.runtime.wire import call_wire, emit_wire, response
from test_wire_helpers import StubWire


class PublicationTests(unittest.IsolatedAsyncioTestCase):
    async def test_pre_admission_call_and_event_refusals_have_local_proof(self):
        wire = StubWire()

        def refuse(path, message):
            raise PublicError("busy", "queue full")

        wire.on_send = refuse
        with self.assertRaises(UnpublishedError) as caught:
            await call_wire(wire, ["read"])
        self.assertEqual(caught.exception.code, "busy")
        self.assertIsInstance(caught.exception.cause, PublicError)
        with self.assertRaises(UnpublishedError):
            emit_wire(wire, ["item"])

    async def test_precancelled_context_has_proof_and_publishes_nothing(self):
        wire = StubWire()
        context = RequestContext(None)
        context.cancelled.set()
        with self.assertRaises(UnpublishedError) as caught:
            await call_wire(wire, ["read"], context=context)
        self.assertEqual(caught.exception.code, "cancelled")
        self.assertEqual(wire.sent, [])

    async def test_received_nested_proof_and_timeout_are_uncertain(self):
        wire = StubWire()
        wire.on_send = lambda path, message: response(message, error=UnpublishedError(PublicError("busy", "nested")))
        with self.assertRaises(PublicError) as caught:
            await call_wire(wire, ["read"])
        self.assertNotIsInstance(caught.exception, UnpublishedError)
        self.assertEqual(caught.exception.code, "busy")
        wire.on_send = None
        with self.assertRaises(PublicError) as caught:
            await call_wire(wire, ["read"], timeout_ms=10)
        self.assertNotIsInstance(caught.exception, UnpublishedError)
        self.assertEqual(caught.exception.code, "request_timeout")
        await asyncio.sleep(0)

    async def test_stripping_proof_preserves_ordinary_error_fields(self):
        plain = PublicError("busy", "full", None)
        proof = unpublished(plain)
        self.assertIsNone(unpublished(None))
        stripped = without_unpublished_proof(proof)
        self.assertNotIsInstance(stripped, UnpublishedError)
        self.assertEqual((stripped.code, str(stripped), stripped.data), ("busy", "full", None))
        self.assertIs(without_unpublished_proof(plain), plain)
