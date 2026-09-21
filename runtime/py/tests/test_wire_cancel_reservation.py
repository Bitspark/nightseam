import asyncio
import unittest

from nightseam.duplex import CloseError, Frame, Message, ReturnAddress, WireError, encode_path, pipe
from nightseam.runtime import Options, Peer, PublicError
from nightseam.runtime.json import encode_object, loads


class _Gate:
    def __init__(self):
        self.entered = asyncio.Event()
        self.release = asyncio.Event()


class _Sink:
    def __init__(self, on_reply=None):
        self.replies = asyncio.Queue()
        self.on_reply = on_reply

    def send(self, path, message):
        if self.on_reply:
            self.on_reply(message.frame)
        self.replies.put_nowait(message.frame)

    def receive(self, path, receiver):
        raise WireError("receiver_exists")

    def close(self, code=1000, reason=""):
        pass

    async def reply(self):
        return await asyncio.wait_for(self.replies.get(), 1)


class _Carrier:
    """An actual peer with a separately drained physical endpoint.

    Only root event admission is paused. The socket writer and remote reader
    keep running, so carrier backpressure cannot disguise the root's bound.
    """

    def __init__(self):
        near, self.remote = pipe()
        self.peer = Peer(near, options=Options(queue_capacity=1, max_pending_requests=1))
        self.wire = self.peer.wire()
        self.frames = asyncio.Queue()
        self.ended = asyncio.get_running_loop().create_future()
        self.gates = []
        self.next_gate = None
        original_emit = self.peer._emit

        async def emit(*args, **kwargs):
            gate, self.next_gate = self.next_gate, None
            if gate is not None:
                gate.entered.set()
                await gate.release.wait()
            return await original_emit(*args, **kwargs)

        self.peer._emit = emit
        self.reader = asyncio.create_task(self.read())

    async def read(self):
        try:
            while True:
                frame = await self.remote.receive()
                self.frames.put_nowait(loads(frame.data))
        except CloseError as error:
            self.ended.set_result(error)

    def pause(self):
        gate = _Gate()
        self.gates.append(gate)
        self.next_gate = gate
        return gate

    async def frame(self):
        return await asyncio.wait_for(self.frames.get(), 1)

    async def respond(self, request, result):
        await self.remote.send(
            Frame("text", encode_object({"version": 1, "kind": "response", "id": request["id"], "result": result}))
        )

    async def close(self):
        for gate in self.gates:
            gate.release.set()
        await self.peer.close()
        self.remote.abort()
        await asyncio.wait_for(self.reader, 1)


def _request(wire, address, request_id="c:17"):
    wire.send(["operation"], Message({"version": 1, "kind": "request", "id": request_id, "params": {}}, address))


def _cancel(wire, address, request_id="c:17"):
    wire.send(["operation"], Message({"version": 1, "kind": "cancel", "id": request_id}, address))


def _event(wire, value):
    wire.send(["operation"], Message({"version": 1, "kind": "event", "data": value}))


class WireCancelReservationTests(unittest.IsolatedAsyncioTestCase):
    def carrier(self):
        carrier = _Carrier()
        self.addAsyncCleanup(carrier.close)
        return carrier

    async def test_known_cancel_has_reserved_admission_and_keeps_fifo_when_data_is_full(self):
        carrier = self.carrier()
        wire = carrier.wire
        sink = _Sink()
        address = ReturnAddress(sink)
        _request(wire, address)
        request = await carrier.frame()
        self.assertEqual((request["kind"], request["method"]), ("request", encode_path(["operation"])))
        gate = carrier.pause()
        _event(wire, "held before admission")
        await asyncio.wait_for(gate.entered.wait(), 1)
        _event(wire, "occupies the sole data slot")
        _cancel(wire, address)
        for _ in range(4):
            _cancel(wire, address)
            _cancel(wire, address, "c:999")
        self.assertEqual(carrier.peer.status, "connected")
        gate.release.set()
        frames = [await carrier.frame() for _ in range(3)]
        self.assertEqual([frame["kind"] for frame in frames], ["event", "event", "cancel"])
        self.assertEqual(
            [frame["data"] for frame in frames[:2]], ["held before admission", "occupies the sole data slot"]
        )
        self.assertEqual(frames[2]["id"], request["id"])
        reply = await sink.reply()
        self.assertEqual((reply["id"], reply["error"]["code"]), ("c:17", "cancelled"))
        # The fence must be the next physical frame: duplicates, unknown ids
        # and already settled controls never acquire another reservation.
        _cancel(wire, address)
        _cancel(wire, address, "c:999")
        _event(wire, "fence")
        fence = await carrier.frame()
        self.assertEqual((fence["kind"], fence["data"]), ("event", "fence"))
        self.assertEqual(carrier.peer.status, "connected")

    async def test_completed_request_retains_queued_cancel_budget_until_control_drains(self):
        carrier = self.carrier()
        wire = carrier.wire
        second = _Sink()
        second_address = ReturnAddress(second)
        reentrant_errors = []

        def reentrant(frame):
            try:
                _request(wire, second_address, "c:18")
            except Exception as error:
                reentrant_errors.append(error)

        first = _Sink(reentrant)
        address = ReturnAddress(first)
        _request(wire, address)
        original = await carrier.frame()
        gate = carrier.pause()
        _event(wire, "held")
        await asyncio.wait_for(gate.entered.wait(), 1)
        _cancel(wire, address)
        # The reply travels through the actual physical reader while the
        # root is blocked. Its callback tries to spend the retained budget.
        await carrier.respond(original, 7)
        self.assertEqual((await first.reply())["result"], 7)
        self.assertEqual(reentrant_errors, [])
        gate.release.set()
        event = await carrier.frame()
        self.assertEqual((event["kind"], event["data"]), ("event", "held"))
        refused = await second.reply()
        self.assertEqual((refused["id"], refused["error"]["code"]), ("c:18", "busy"))
        first.on_reply = None
        _request(wire, address)  # Reuse the original capability and local id.
        fresh = await carrier.frame()
        self.assertEqual(fresh["kind"], "request")
        self.assertNotEqual(fresh["id"], original["id"])
        await carrier.respond(fresh, 9)
        self.assertEqual((await first.reply())["result"], 9)
        _event(wire, "fence")
        fence = await carrier.frame()
        self.assertEqual((fence["kind"], fence["data"]), ("event", "fence"))
        self.assertEqual(carrier.peer.status, "connected")

    async def test_control_reservations_never_increase_data_capacity_and_overflow_is_isolated(self):
        carrier, independent = self.carrier(), self.carrier()
        wire = carrier.wire
        sink = _Sink()
        address = ReturnAddress(sink)
        _request(wire, address)
        self.assertEqual((await carrier.frame())["kind"], "request")
        gate = carrier.pause()
        _event(wire, "held")
        await asyncio.wait_for(gate.entered.wait(), 1)
        _event(wire, "only data slot")
        _cancel(wire, address)
        with self.assertRaises(PublicError) as caught:
            _event(wire, "overflow")
        self.assertEqual(caught.exception.code, "busy")
        closure = await asyncio.wait_for(carrier.ended, 1)
        self.assertEqual(closure.code, 4011)
        self.assertEqual((await sink.reply())["error"]["code"], "disconnected")
        self.assertEqual(carrier.peer.status, "disconnected")
        self.assertTrue(carrier.frames.empty())  # The paused data never escaped.
        other = _Sink()
        _request(independent.wire, ReturnAddress(other))
        request = await independent.frame()
        self.assertEqual(request["kind"], "request")
        await independent.respond(request, "still usable")
        self.assertEqual((await other.reply())["result"], "still usable")
        self.assertEqual(independent.peer.status, "connected")


if __name__ == "__main__":
    unittest.main()
