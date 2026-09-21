import asyncio
import inspect
import unittest

from nightseam.duplex import Message, ReturnAddress
from nightseam.runtime import PublicError, RawJSON, RequestContext
from nightseam.runtime.wire import (
    WireDispatchContext,
    WireHandlers,
    call_wire,
    clone_context,
    compose_cancellation,
    emit_wire,
    handle_wire,
    inherit_wire_context,
    on_wire_event,
    outgoing_trace,
    profile_frame,
    register_wire,
    response,
    set_wire_context,
    wire_context,
    wire_event_context,
    with_outgoing_trace,
    with_wire_event_context,
)


class StubWire:
    def __init__(self):
        self.sent = []
        self.registrations = []
        self.closed = []
        self.removed = 0
        self.on_send = None
        self.tasks = set()

    def send(self, path, message):
        self.sent.append((list(path), message))
        if self.on_send:
            self.on_send(path, message)

    def receive(self, path, receiver):
        self.registrations.append((list(path), receiver))
        active = True

        def detach():
            nonlocal active
            if active:
                active = False
                self.removed += 1

        return detach

    def close(self, code=1000, reason=""):
        self.closed.append((code, reason))

    def dispatch(self, path, message):
        pending = self.registrations[0][1].message(path, message)
        if inspect.isawaitable(pending):
            task = asyncio.create_task(pending)
            self.tasks.add(task)
            task.add_done_callback(self.tasks.discard)


def request(returning, request_id="c:1", params=None, **fields):
    return Message({"version": 1, "kind": "request", "id": request_id, "params": params, **fields}, returning)


class WireHelperTests(unittest.IsolatedAsyncioTestCase):
    async def test_call_and_emit_snapshot_payloads_and_preserve_presence(self):
        wire = StubWire()

        def answer(path, message):
            if message.frame["kind"] == "request":
                response(message, message.frame["params"])

        wire.on_send = answer
        self.assertEqual(await call_wire(wire, ["echo"]), {})
        for value in (None, False, 0, "", [], {}):
            self.assertEqual(await call_wire(wire, ["echo"], value), value)
        self.assertEqual(await call_wire(wire, ["echo"], RawJSON("1e3")), 1000)
        data = {"nested": [1]}
        emit_wire(wire, ["item"], data, meta={"tenant": "one", "nightseam.reserved": "omit"})
        data["nested"].append(2)
        self.assertEqual(wire.sent[-1][1].frame["data"], {"nested": [1]})
        self.assertEqual(wire.sent[-1][1].frame["meta"], {"tenant": "one"})
        emit_wire(wire, [], None)
        self.assertIsNone(wire.sent[-1][1].frame["data"])

    async def test_timeout_and_task_cancellation_reuse_return_capability(self):
        for timeout in (True, False):
            wire = StubWire()
            task = asyncio.create_task(call_wire(wire, ["hold"], timeout_ms=20 if timeout else 1000))
            await asyncio.sleep(0)
            if timeout:
                with self.assertRaises(PublicError) as caught:
                    await task
                self.assertEqual(caught.exception.code, "request_timeout")
            else:
                task.cancel()
                with self.assertRaises(asyncio.CancelledError):
                    await task
            self.assertEqual([message.frame["kind"] for _, message in wire.sent], ["request", "cancel"])
            first, second = [message for _, message in wire.sent]
            self.assertIs(first.return_address, second.return_address)
            self.assertEqual(first.frame["traceparent"], second.frame["traceparent"])
            self.assertEqual(wire.closed, [])

    async def test_register_returns_real_handler_lifetime_and_preserves_public_refusal(self):
        wire, returning = StubWire(), StubWire()
        entered, release = asyncio.Event(), asyncio.Event()

        async def handle(value, context):
            entered.set()
            await release.wait()
            raise PublicError("cancelled", "application refusal")

        register_wire(wire, ["hold"], WireHandlers(request=handle))
        receiver = wire.registrations[0][1]
        incoming = request(ReturnAddress(returning))
        lifetime = receiver.message(["hold"], incoming)
        self.assertTrue(inspect.isawaitable(lifetime))
        self.assertFalse(entered.is_set())
        task = asyncio.create_task(lifetime)
        await entered.wait()
        receiver.message(["hold"], Message({"version": 1, "kind": "cancel", "id": "c:1"}, incoming.return_address))
        self.assertFalse(task.done())
        self.assertEqual(returning.sent, [])
        release.set()
        await task
        self.assertEqual(returning.sent[0][1].frame["error"]["message"], "application refusal")

    async def test_capability_identity_separates_equal_ids_even_on_same_return_wire(self):
        wire, returning = StubWire(), StubWire()
        release = asyncio.Event()
        contexts = []

        async def hold(value, context):
            contexts.append(context)
            await release.wait()
            return value

        handle_wire(wire, ["hold"], hold)
        receiver = wire.registrations[0][1]
        a, b = ReturnAddress(returning), ReturnAddress(returning)
        first = asyncio.create_task(receiver.message([], request(a, params=1)))
        second = asyncio.create_task(receiver.message([], request(b, params=2)))
        await asyncio.sleep(0)
        self.assertEqual(len(contexts), 2)
        receiver.message([], Message({"version": 1, "kind": "cancel", "id": "c:1"}, a))
        self.assertTrue(contexts[0].cancelled.is_set())
        self.assertFalse(contexts[1].cancelled.is_set())
        release.set()
        await asyncio.gather(first, second)
        self.assertEqual([message.frame.get("result") for _, message in returning.sent], [None, 2])

    async def test_private_request_context_survives_but_metadata_never_propagates_implicitly(self):
        wire, returning = StubWire(), StubWire()
        source = RequestContext(None, "s:42", meta={"tenant": "incoming"})
        source.verified = object()
        source.trace = {"traceparent": "00-" + "1" * 32 + "-" + "2" * 16 + "-01"}
        address = ReturnAddress(returning)
        cleanup = set_wire_context(address, WireDispatchContext(source, max_frame_bytes=1024))
        copied = ReturnAddress(returning)
        inherited = inherit_wire_context(address, copied)
        self.assertIs(wire_context(copied).context, source)
        seen = []

        def handle(value, context):
            seen.append(context)
            emit_wire(wire, ["out"], context=context)
            return value

        handle_wire(wire, ["operation"], handle)
        await wire.registrations[0][1].message([], request(address, params=7, meta={"tenant": "incoming"}))
        self.assertIs(seen[0].verified, source.verified)
        self.assertEqual(seen[0].request_id, "s:42")
        self.assertEqual(seen[0].meta, {"tenant": "incoming"})
        self.assertNotIn("meta", wire.sent[0][1].frame)
        self.assertIs(clone_context(source).verified, source.verified)
        cleanup()
        inherited()
        self.assertIsNone(wire_context(address))
        self.assertIsNone(wire_context(copied))

    async def test_event_context_is_private_and_errors_close_with_sanitized_reason(self):
        wire = StubWire()
        source = RequestContext(None)
        source.verified = object()
        seen = []
        panics = []

        async def event(value, context):
            seen.append(context)
            raise ValueError("private payload must not appear in close reason")

        on_wire_event(wire, ["item"], event)
        receiver = wire.registrations[0][1]
        message = Message({"version": 1, "kind": "event", "data": None, "meta": {"verified": "forged"}})
        self.assertIsNone(wire_event_context(message))
        carried = with_wire_event_context(message, source, panics.append)
        self.assertIs(wire_event_context(carried).context, source)
        await receiver.message([], carried)
        self.assertIs(seen[0].verified, source.verified)
        self.assertEqual(wire.closed, [(1002, "wire event rejected")])
        self.assertEqual(len(panics), 1)

    async def test_outgoing_trace_identity_survives_snapshots_without_becoming_json(self):
        class Trace(dict):
            pass

        trace = Trace(traceparent="00-" + "1" * 32 + "-" + "2" * 16 + "-01")
        trace.private = object()
        frame = with_outgoing_trace({"version": 1, "kind": "event", "data": None, **trace}, trace)
        copied = profile_frame(frame, "4:item", 1024)
        self.assertIs(outgoing_trace(copied), trace)
        self.assertEqual(set(copied), {"version", "kind", "data", "traceparent"})
        cancelled = asyncio.Event()
        combined, cleanup = compose_cancellation(cancelled, asyncio.Event())
        cancelled.set()
        self.assertTrue(combined.is_set())
        cleanup()
        await asyncio.wait_for(combined.wait(), 1)

    async def test_return_validation_and_oversized_result_use_internal_fallback(self):
        wire = StubWire()

        def answer(path, message):
            address = message.return_address
            with self.assertRaises(PublicError):
                address.wire.send([], Message({"version": 1, "kind": "response", "id": "c:2", "result": 0}))
            response(message, "valid")

        wire.on_send = answer
        self.assertEqual(await call_wire(wire, ["echo"]), "valid")
        returning = StubWire()
        returning.on_send = lambda path, message: profile_frame(message.frame, "", 180)
        outcome = response(request(ReturnAddress(returning)), "x" * 1000)
        self.assertEqual(outcome.code, "internal")
        self.assertEqual(returning.sent[-1][1].frame["error"]["code"], "internal")

    async def test_malformed_frames_refuse_without_mutating_input(self):
        for fields in ({"method": "extra"}, {"version": True}, {"data": "\ud800"}, {"meta": {"nightseam.x": "x"}}):
            frame = {"version": 1, "kind": "event", "data": None, **fields}
            with self.assertRaises(PublicError):
                profile_frame(frame, "1:x")
        with self.assertRaises(PublicError):
            profile_frame({"version": 1, "kind": "event", "data": "x" * 100}, "1:x", 20)
