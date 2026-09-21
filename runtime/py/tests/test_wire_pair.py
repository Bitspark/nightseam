import asyncio
import unittest

from nightseam.duplex import Message, Receiver, ReturnAddress, WireError
from nightseam.runtime import Options, PublicError
from nightseam.runtime.wire_pair import wire_pair


class Sink:
    def __init__(self, send=None):
        self.replies = asyncio.Queue()
        self.callback = send

    def send(self, path, message):
        if self.callback:
            self.callback(path, message)
        self.replies.put_nowait(message.frame)

    def receive(self, path, receiver):
        raise WireError("receiver_exists")

    def close(self, code=1000, reason=""):
        pass

    async def reply(self):
        return await asyncio.wait_for(self.replies.get(), 1)


def request(wire, address, request_id="c:1", path=("call",), params=None):
    wire.send(path, Message({"version": 1, "kind": "request", "id": request_id, "params": params}, address))


def cancel(wire, address, request_id="c:1", path=("call",)):
    wire.send(path, Message({"version": 1, "kind": "cancel", "id": request_id}, address))


def emit(wire, value=None, path=("event",)):
    wire.send(path, Message({"version": 1, "kind": "event", "data": value}))


def respond(message, value=None):
    message.return_address.wire.send(
        [], Message({"version": 1, "kind": "response", "id": message.frame["id"], "result": value})
    )


class WirePairTests(unittest.IsolatedAsyncioTestCase):
    def pair(self, **options):
        left, right = wire_pair(Options(**options))
        self.addCleanup(left.close)
        return left, right

    async def test_dispatch_is_deferred_and_snapshots_paths_payloads_and_metadata(self):
        left, right = self.pair()
        received = asyncio.Queue()
        right.receive([], Receiver(namespace=True, message=lambda path, message: received.put_nowait((path, message))))
        path = ["event"]
        frame = {"version": 1, "kind": "event", "data": {"nested": ["before"]}, "meta": {"name": "before"}}
        left.send(path, Message(frame))
        self.assertTrue(received.empty())
        path[0] = "changed"
        frame["data"]["nested"][0] = "changed"
        frame["meta"]["name"] = "changed"
        actual_path, actual = await asyncio.wait_for(received.get(), 1)
        self.assertEqual(actual_path, ["event"])
        self.assertEqual(actual.frame["data"], {"nested": ["before"]})
        self.assertEqual(actual.frame["meta"], {"name": "before"})

    async def test_same_identifier_from_distinct_capabilities_has_independent_calls(self):
        left, right = self.pair()
        held = asyncio.Queue()
        right.receive(["call"], Receiver(message=lambda path, message: held.put_nowait(message)))
        sink = Sink()
        first, second = ReturnAddress(sink), ReturnAddress(sink)
        request(left, first, params=1)
        request(left, second, params=2)
        a, b = await held.get(), await held.get()
        self.assertIsNot(a.return_address, b.return_address)
        self.assertIsNot(a.return_address, first)
        respond(a, "first")
        respond(b, "second")
        self.assertEqual((await sink.reply())["result"], "first")
        self.assertEqual((await sink.reply())["result"], "second")

    async def test_request_starts_before_later_event_without_blocking_dispatch(self):
        left, right = self.pair()
        seen = []
        release, event_seen = asyncio.Event(), asyncio.Event()
        self.addCleanup(release.set)

        async def handler(path, message):
            seen.append("request")
            await release.wait()
            respond(message, "done")

        def event(path, message):
            seen.append("event")
            event_seen.set()

        right.receive(["call"], Receiver(message=handler))
        right.receive(["event"], Receiver(message=event))
        sink = Sink()
        request(left, ReturnAddress(sink))
        emit(left)
        await asyncio.wait_for(event_seen.wait(), 1)
        self.assertEqual(seen, ["request", "event"])
        release.set()
        self.assertEqual((await sink.reply())["result"], "done")

    async def test_reverse_calls_use_no_peer_and_bounded_response_fallback(self):
        from nightseam.runtime.wire import call_wire, handle_wire

        left, right = self.pair(max_frame_bytes=512)
        handle_wire(left, ["reverse"], lambda value, context: value)

        async def forward(value, context):
            return await call_wire(right, ["reverse"], value)

        handle_wire(right, ["call"], forward)
        self.assertEqual(await call_wire(left, ["call"], 7), 7)
        handle_wire(right, ["large"], lambda value, context: "x" * 2048)
        with self.assertRaises(PublicError) as large:
            await call_wire(left, ["large"])
        self.assertEqual(large.exception.code, "internal")

    async def test_received_context_and_custom_trace_survive_local_admission(self):
        from nightseam.runtime.wire import (
            WireDispatchContext,
            WireEventContext,
            WireRequestContext,
            set_wire_context,
            wire_context,
            wire_event_context,
            with_wire_event_context,
        )

        class Propagator:
            def extract(self, context, trace):
                context.verified = object()
                context.trace = {"private": "local propagation"}

        left, right = self.pair(propagator=Propagator())
        held = asyncio.Queue()
        right.receive([], Receiver(namespace=True, message=lambda path, message: held.put_nowait(message)))
        sink = Sink()
        address = ReturnAddress(sink)
        source = WireRequestContext(wire=left, request_id="physical:17", cancelled=asyncio.Event())
        source.verified = object()
        cleanup = set_wire_context(address, WireDispatchContext(source))
        self.addCleanup(cleanup)
        left.send(
            ["call"],
            Message(
                {"version": 1, "kind": "request", "id": "c:1", "params": None, "meta": {"explicit": "yes"}}, address
            ),
        )
        admitted = await held.get()
        context = wire_context(admitted.return_address).context
        self.assertIs(context.verified, source.verified)
        self.assertEqual(context.request_id, "physical:17")
        self.assertEqual(context.meta, {"explicit": "yes"})
        source.cancelled.set()
        await asyncio.wait_for(context.cancelled.wait(), 1)
        respond(admitted)
        await sink.reply()
        event_context = WireEventContext(wire=left)
        event_context.verified = object()
        outgoing = with_wire_event_context(Message({"version": 1, "kind": "event", "data": 7}), event_context)
        left.send(["event"], outgoing)
        delivered = await held.get()
        self.assertIs(wire_event_context(delivered).context.verified, event_context.verified)
        emit(left)
        fresh = await held.get()
        self.assertEqual(wire_event_context(fresh).context.trace, {"private": "local propagation"})

    async def test_invalid_frames_and_missing_return_are_refused_without_consuming_capacity(self):
        left, right = self.pair(queue_capacity=1)
        for message in (
            Message({"version": 1, "kind": "request", "id": "c:1", "params": None}),
            Message({"version": 1, "kind": "response", "id": "c:1", "result": None}),
            Message({"version": 1, "kind": "event", "data": None, "unknown": True}),
        ):
            with self.subTest(frame=message.frame), self.assertRaises(PublicError):
                left.send([], message)
        sink = Sink()
        right.receive(["call"], Receiver(message=lambda path, message: respond(message, "usable")))
        request(left, ReturnAddress(sink))
        self.assertEqual((await sink.reply())["result"], "usable")

    async def test_exact_then_longest_namespace_and_duplicate_registration(self):
        left, right = self.pair()
        seen = []

        def receiver(label, namespace=True):
            def deliver(path, message):
                seen.append((label, path))
                respond(message, label)

            return Receiver(namespace=namespace, message=deliver)

        right.receive([], receiver("root"))
        right.receive(["a"], receiver("prefix"))
        detach = right.receive(["a", "b"], receiver("exact", False))
        with self.assertRaises(WireError):
            right.receive(["a", "b"], receiver("duplicate", False))
        sink = Sink()
        address = ReturnAddress(sink)
        request(left, address, path=["a", "b"])
        self.assertEqual((await sink.reply())["result"], "exact")
        detach()
        detach()
        request(left, address, path=["a", "b"])
        self.assertEqual((await sink.reply())["result"], "prefix")
        request(left, address, path=["other"])
        self.assertEqual((await sink.reply())["result"], "root")
        self.assertEqual(seen, [("exact", ["a", "b"]), ("prefix", ["a", "b"]), ("root", ["other"])])

    async def test_pending_capacity_is_retained_through_response_and_reusable(self):
        left, right = self.pair(max_pending_requests=1)
        held = asyncio.Queue()
        right.receive(["call"], Receiver(message=lambda path, message: held.put_nowait(message)))
        sink = Sink()
        address = ReturnAddress(sink)
        request(left, address)
        first = await held.get()
        request(left, address, "c:2")
        self.assertEqual((await sink.reply())["error"]["code"], "busy")
        respond(first, 1)
        self.assertEqual((await sink.reply())["result"], 1)
        request(left, address, "c:3")
        respond(await held.get(), 3)
        self.assertEqual((await sink.reply())["result"], 3)

    async def test_cancel_has_reserved_admission_preserves_fifo_and_original_receiver(self):
        left, right = self.pair(queue_capacity=1, max_pending_requests=1)
        held = asyncio.Queue()
        seen = []

        def receiver(path, message):
            seen.append((message.frame["kind"], path))
            held.put_nowait(message)

        detach = right.receive(["call"], Receiver(message=receiver))
        sink = Sink()
        address = ReturnAddress(sink)
        request(left, address)
        original = await held.get()
        entered, release = asyncio.Event(), asyncio.Event()
        self.addCleanup(release.set)

        async def event(path, message):
            seen.append(("event", message.frame["data"]))
            if message.frame["data"] == 1:
                entered.set()
                await release.wait()

        right.receive(["event"], Receiver(message=event))
        emit(left, 1)
        await entered.wait()
        emit(left, 2)
        cancel(left, address, path=["different"])
        for _ in range(4):
            cancel(left, address)
            cancel(left, address, "unknown")
        detach()
        right.receive(["call"], Receiver(message=lambda path, message: self.fail("cancel was rerouted")))
        release.set()
        control = await asyncio.wait_for(held.get(), 1)
        self.assertIs(control.return_address, original.return_address)
        self.assertEqual(seen, [("request", ["call"]), ("event", 1), ("event", 2), ("cancel", ["call"])])
        respond(original)
        await sink.reply()
        cancel(left, address)
        emit(left, 3)
        await asyncio.sleep(0.01)
        self.assertEqual(seen[-1], ("event", 3))

    async def test_completed_request_retains_queued_cancel_reservation_until_drain(self):
        left, right = self.pair(queue_capacity=1, max_pending_requests=1)
        held = asyncio.Queue()
        right.receive(["call"], Receiver(message=lambda path, message: held.put_nowait(message)))
        second = Sink()
        next_address = ReturnAddress(second)
        first = Sink(lambda path, message: request(left, next_address, "c:2"))
        address = ReturnAddress(first)
        request(left, address)
        original = await held.get()
        entered, release = asyncio.Event(), asyncio.Event()
        self.addCleanup(release.set)

        async def event(path, message):
            entered.set()
            await release.wait()

        right.receive(["event"], Receiver(message=event))
        emit(left)
        await entered.wait()
        cancel(left, address)
        respond(original, 7)
        self.assertEqual((await first.reply())["result"], 7)
        release.set()
        self.assertEqual((await second.reply())["error"]["code"], "busy")
        first.callback = None
        request(left, address)
        fresh = await asyncio.wait_for(held.get(), 1)
        self.assertEqual(fresh.frame["kind"], "request")
        respond(fresh, 9)
        self.assertEqual((await first.reply())["result"], 9)

    async def test_reserved_control_capacity_does_not_increase_data_capacity_or_close_other_pairs(self):
        left, right = self.pair(queue_capacity=1)
        other_left, other_right = self.pair()
        other_sink = Sink()
        other_right.receive(["call"], Receiver(message=lambda path, message: respond(message, "alive")))
        entered, release, closed = asyncio.Event(), asyncio.Event(), asyncio.Event()
        self.addCleanup(release.set)

        async def blocked(path, message):
            entered.set()
            await release.wait()

        right.receive(["event"], Receiver(message=blocked, closed=lambda code, reason: closed.set()))
        emit(left)
        await entered.wait()
        emit(left)
        with self.assertRaises(PublicError) as overflow:
            emit(left)
        self.assertEqual(overflow.exception.code, "busy")
        self.assertFalse(closed.is_set())
        await asyncio.wait_for(closed.wait(), 1)
        request(other_left, ReturnAddress(other_sink))
        self.assertEqual((await other_sink.reply())["result"], "alive")

    async def test_deadline_and_early_response_retain_actual_handler_capacity(self):
        for early_response in (False, True):
            with self.subTest(early_response=early_response):
                left, right = self.pair(request_timeout_ms=20, max_concurrent_handlers=1)
                release, entered = asyncio.Event(), asyncio.Event()
                self.addCleanup(release.set)
                seen = []

                async def handler(path, message):
                    if message.frame["kind"] != "request":
                        return
                    seen.append(message)
                    entered.set()
                    if early_response:
                        respond(message, 1)
                    await release.wait()
                    if not early_response:
                        try:
                            respond(message, 2)
                        except PublicError:
                            pass

                right.receive(["call"], Receiver(message=handler))
                sink = Sink()
                address = ReturnAddress(sink)
                request(left, address)
                await entered.wait()
                first = await sink.reply()
                self.assertEqual(
                    first.get("result") if early_response else first["error"]["code"],
                    1 if early_response else "cancelled",
                )
                request(left, address, "c:2")
                self.assertEqual((await sink.reply())["error"]["code"], "busy")
                self.assertEqual(len(seen), 1)
                release.set()
                await asyncio.sleep(0.01)
                request(left, address, "c:3")
                self.assertEqual((await sink.reply())["result"], 1 if early_response else 2)

    async def test_stalled_event_closes_with_observation_despite_throwing_observer(self):
        events = []

        def observer(event):
            events.append(event)
            raise RuntimeError("observer failure")

        left, right = self.pair(write_timeout_ms=20, observer=observer)
        release, ended = asyncio.Event(), asyncio.Event()
        self.addCleanup(release.set)

        async def handler(path, message):
            await release.wait()

        right.receive(["event"], Receiver(message=handler, closed=lambda code, reason: ended.set()))
        emit(left)
        await asyncio.wait_for(ended.wait(), 1)
        self.assertEqual([event["type"] for event in events], ["backpressure", "connection.closed"])
        self.assertTrue(events[0]["stalled"])
        with self.assertRaises(PublicError) as closed:
            emit(left)
        self.assertEqual(closed.exception.code, "disconnected")

    async def test_return_failure_retires_request_and_strips_unpublished_proof(self):
        from nightseam.runtime.publication import UnpublishedError, unpublished

        left, right = self.pair(max_pending_requests=1)
        held = asyncio.Queue()
        right.receive(["call"], Receiver(message=lambda path, message: held.put_nowait(message)))
        failure = RuntimeError("return refused")

        def fail(path, message):
            raise unpublished(failure)

        request(left, ReturnAddress(Sink(fail)))
        message = await held.get()
        with self.assertRaises(RuntimeError) as result:
            respond(message)
        self.assertIs(result.exception, failure)
        self.assertNotIsInstance(result.exception, UnpublishedError)
        sink = Sink()
        request(left, ReturnAddress(sink))
        respond(await held.get(), "reused")
        self.assertEqual((await sink.reply())["result"], "reused")


if __name__ == "__main__":
    unittest.main()
