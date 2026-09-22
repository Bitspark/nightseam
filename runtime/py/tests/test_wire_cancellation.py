import asyncio
import unittest

from nightseam.duplex import Frame, encode_path, mount, pipe
from nightseam.runtime import (
    Options,
    Peer,
    PublicError,
    call_wire,
    forward_wire,
    handle_wire,
    wire_pair,
)
from nightseam.runtime.json import encode_object, loads
from routing import dispatcher, select_endpoint


class WireCancellationTests(unittest.IsolatedAsyncioTestCase):
    async def setup_route(self, composed=False, *, deadline=2000, local_deadline=2000):
        near, far = pipe()
        observations = []
        peer = Peer(far, "server", Options(request_timeout_ms=deadline, observer=observations.append))
        self.addCleanup(near.abort)
        self.addAsyncCleanup(peer.close)
        binding = peer.wire()
        if composed:
            access, binding = wire_pair(Options(request_timeout_ms=local_deadline, max_concurrent_handlers=1))
            self.addCleanup(access.close)
            self.addCleanup(forward_wire(peer.wire(), select_endpoint(mount({"local": access}), ["local"])))
            binding = select_endpoint(mount({"binding": binding}), ["binding"])
        return near, peer, binding, observations

    async def send(self, connection, kind):
        frame = {"version": 1, "kind": kind, "id": "c:1"}
        if kind == "request":
            frame.update(method=encode_path(["work"]), params={})
        await connection.send(Frame("text", encode_object(frame)))

    def incoming_ends(self, observations):
        return [event for event in observations if event["type"] == "request.ended" and event["incoming"]]

    async def test_explicit_cancel_waits_for_body_and_preserves_public_failure(self):
        for composed in (False, True):
            for code in (None, "declined", "cancelled"):
                with self.subTest(composed=composed, code=code):
                    near, peer, binding, observations = await self.setup_route(composed)
                    entered, cancelled, release = asyncio.Event(), asyncio.Event(), asyncio.Event()

                    async def work(value, context):
                        entered.set()
                        await context.cancelled.wait()
                        cancelled.set()
                        await release.wait()
                        if code is not None:
                            raise PublicError(code, "application decision", {"source": "body"})
                        return "completed"

                    handle_wire(dispatcher(binding), ["work"], work)
                    await self.send(near, "request")
                    await asyncio.wait_for(entered.wait(), 1)
                    reply = asyncio.create_task(near.receive())
                    try:
                        await self.send(near, "cancel")
                        await asyncio.wait_for(cancelled.wait(), 1)
                        for _ in range(12):
                            await asyncio.sleep(0)
                        self.assertFalse(reply.done(), "explicit cancellation answered before the body completed")
                        self.assertEqual(self.incoming_ends(observations), [])
                    finally:
                        release.set()
                    response = loads((await asyncio.wait_for(reply, 1)).data)
                    self.assertEqual(response["error"]["code"], code or "cancelled")
                    if code is not None:
                        self.assertEqual(response["error"]["message"], "application decision")
                        self.assertEqual(response["error"]["data"], {"source": "body"})
                    ended = self.incoming_ends(observations)
                    self.assertEqual(len(ended), 1)
                    self.assertEqual(ended[0]["outcome"], "error" if code else "cancelled")
                    await peer.close()

    async def test_receiver_deadline_answers_once_and_retains_application_capacity(self):
        near, peer, binding, observations = await self.setup_route(deadline=25)
        entered, release = asyncio.Event(), asyncio.Event()

        async def work(value, context):
            entered.set()
            await release.wait()
            raise PublicError("declined", "too late")

        handle_wire(dispatcher(binding), ["work"], work)
        await self.send(near, "request")
        await asyncio.wait_for(entered.wait(), 1)
        try:
            response = loads((await asyncio.wait_for(near.receive(), 1)).data)
            self.assertEqual(response["error"]["code"], "cancelled")
            self.assertEqual(len(peer._incoming), 1)
            self.assertEqual([event["outcome"] for event in self.incoming_ends(observations)], ["timeout"])
        finally:
            release.set()
        for _ in range(16):
            await asyncio.sleep(0)
        self.assertEqual(len(peer._incoming), 0)
        self.assertEqual(len(self.incoming_ends(observations)), 1)

    async def test_inner_receiver_deadline_is_a_public_error_at_outer_carrier(self):
        near, peer, binding, observations = await self.setup_route(True, local_deadline=25)
        entered, release = asyncio.Event(), asyncio.Event()
        physical_context = None

        async def work(value, context):
            nonlocal physical_context
            physical_context = peer._incoming["c:1"].context
            entered.set()
            await release.wait()
            return "too late"

        handle_wire(dispatcher(binding), ["work"], work)
        await self.send(near, "request")
        await asyncio.wait_for(entered.wait(), 1)
        try:
            response = loads((await asyncio.wait_for(near.receive(), 1)).data)
            self.assertEqual(response["error"]["code"], "cancelled")
            self.assertEqual([event["outcome"] for event in self.incoming_ends(observations)], ["error"])
            self.assertFalse(physical_context.cancelled.is_set())
        finally:
            release.set()

    async def test_explicit_cancel_returns_outgoing_caller_before_body_completion(self):
        near, far = pipe()
        client, server = Peer(near), Peer(far, "server")
        self.addAsyncCleanup(client.close)
        self.addAsyncCleanup(server.close)
        entered, release = asyncio.Event(), asyncio.Event()

        async def work(value, context):
            entered.set()
            await release.wait()
            return None

        handle_wire(dispatcher(server.wire()), ["work"], work)
        call = asyncio.create_task(call_wire(client.wire(), ["work"]))
        await asyncio.wait_for(entered.wait(), 1)
        try:
            call.cancel()
            with self.assertRaises(asyncio.CancelledError):
                await asyncio.wait_for(call, 0.3)
            self.assertEqual(len(server._incoming), 1)
        finally:
            release.set()

    async def test_remote_deadline_cannot_claim_a_local_timeout_cause(self):
        near, peer, _, observations = await self.setup_route()
        middle, far = pipe()
        forwarding = Peer(middle)
        destination = Peer(far, "server", Options(request_timeout_ms=25))
        self.addAsyncCleanup(forwarding.close)
        self.addAsyncCleanup(destination.close)
        self.addCleanup(forward_wire(peer.wire(), forwarding.wire()))
        entered, release = asyncio.Event(), asyncio.Event()

        async def work(value, context):
            entered.set()
            await release.wait()
            return None

        handle_wire(dispatcher(destination.wire()), ["work"], work)
        await self.send(near, "request")
        await asyncio.wait_for(entered.wait(), 1)
        try:
            response = loads((await asyncio.wait_for(near.receive(), 1)).data)
            self.assertEqual(response["error"]["code"], "cancelled")
            self.assertEqual([event["outcome"] for event in self.incoming_ends(observations)], ["error"])
        finally:
            release.set()


if __name__ == "__main__":
    unittest.main()
