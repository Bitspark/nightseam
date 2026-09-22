import asyncio
import unittest

from nightseam.duplex import mount, pipe
from nightseam.runtime import (
    ABSENT,
    DefaultPropagator,
    Options,
    Peer,
    PublicError,
    call_wire,
    emit_wire,
    forward_wire,
    handle_wire,
    on_wire_event,
    wire_pair,
)
from routing import dispatcher, select_endpoint


class WireContextTests(unittest.IsolatedAsyncioTestCase):
    async def physical(self, options=None):
        a, b = pipe()
        client, server = Peer(a), Peer(b, "server", options)
        self.addAsyncCleanup(client.close)
        self.addAsyncCleanup(server.close)
        return client, server

    def local(self):
        access, binding = wire_pair()
        self.addCleanup(access.close)
        return access, binding

    async def test_verified_request_context_survives_composition_but_reverse_metadata_is_explicit(self):
        verified = object()

        class Verifier(DefaultPropagator):
            def extract(self, context, trace):
                super().extract(context, trace)
                context.verified = verified

        client, server = await self.physical(Options(propagator=Verifier()))
        access, binding = self.local()
        self.addCleanup(forward_wire(server.wire(), select_endpoint(mount({"local": access}), ["local"])))
        seen = []

        def reverse(value, context):
            seen.append(context)
            return value

        handle_wire(dispatcher(client.wire()), ["reverse"], reverse)

        async def model(value, context):
            self.assertIs(context.verified, verified)
            self.assertIs(context.peer, server)
            self.assertEqual(context.meta, {"credential": "one-call"})
            first = await call_wire(binding, ["reverse"], value, context=context)
            second = await call_wire(binding, ["reverse"], value, context=context, meta=context.meta)
            return [first, second]

        handle_wire(dispatcher(binding), ["check"], model)
        self.assertEqual(await call_wire(client.wire(), ["check"], 7, meta={"credential": "one-call"}), [7, 7])
        self.assertIs(seen[0].meta, ABSENT)
        self.assertEqual(seen[1].meta, {"credential": "one-call"})
        self.assertFalse(hasattr(seen[0], "verified"))
        self.assertFalse(hasattr(seen[1], "verified"))

    async def test_verified_event_context_survives_local_forwarding_without_becoming_outgoing_metadata(self):
        verified = object()

        class Verifier(DefaultPropagator):
            def extract(self, context, trace):
                super().extract(context, trace)
                context.verified = verified

        client, server = await self.physical(Options(propagator=Verifier()))
        access, binding = self.local()
        self.addCleanup(forward_wire(server.wire(), access))
        observed = asyncio.Queue()
        selected = select_endpoint(mount({"model": binding}), ["model", "events"])
        on_wire_event(dispatcher(selected), ["change"], lambda value, context: observed.put_nowait(context))
        emit_wire(client.wire(), ["events", "change"], meta={"tenant": "explicit"})
        context = await asyncio.wait_for(observed.get(), 1)
        self.assertIs(context.verified, verified)
        self.assertIs(context.peer, server)
        self.assertEqual(context.meta, {"tenant": "explicit"})
        on_wire_event(dispatcher(client.wire()), ["outgoing"], lambda value, context: observed.put_nowait(context))
        emit_wire(server.wire(), ["outgoing"], context=context)
        emit_wire(server.wire(), ["outgoing"], context=context, meta=context.meta)
        first, second = await asyncio.wait_for(observed.get(), 1), await asyncio.wait_for(observed.get(), 1)
        self.assertIs(first.meta, ABSENT)
        self.assertEqual(second.meta, {"tenant": "explicit"})
        self.assertFalse(hasattr(first, "verified"))
        self.assertFalse(hasattr(second, "verified"))

    async def test_custom_received_context_cannot_rewrite_forwarded_trace_or_cross_physical_hop(self):
        original = {"traceparent": "00-11111111111111111111111111111111-2222222222222222-01"}
        replacement = {"traceparent": "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01"}

        class Sender(DefaultPropagator):
            def inject(self, context):
                return original

        class Rewriter(DefaultPropagator):
            def extract(self, context, trace):
                context.trace = replacement
                context.verified = True

        first_client, first_server = await self.physical(Options(propagator=Rewriter()))
        second_client, second_server = await self.physical()
        self.addCleanup(forward_wire(first_server.wire(), second_client.wire()))
        observed = asyncio.Queue()
        on_wire_event(dispatcher(second_server.wire()), ["event"], lambda value, context: observed.put_nowait(context))
        emit_wire(first_client.wire(), ["event"], meta={"explicit": "yes"}, propagator=Sender())
        context = await asyncio.wait_for(observed.get(), 1)
        self.assertEqual(context.trace, original)
        self.assertEqual(context.meta, {"explicit": "yes"})
        self.assertFalse(hasattr(context, "verified"))
        handle_wire(dispatcher(second_server.wire()), ["request"], lambda value, context: context.trace)
        self.assertEqual(await call_wire(first_client.wire(), ["request"], propagator=Sender()), original)

    async def test_metadata_cannot_fabricate_private_context_for_an_effect_guard(self):
        client, server = await self.physical()
        access, binding = self.local()
        self.addCleanup(forward_wire(server.wire(), access))
        effects = []

        def guard(value, context):
            if not getattr(context, "verified", False):
                raise PublicError("denied", "unverified context")
            effects.append(value)

        handle_wire(dispatcher(binding), ["guard"], guard)
        with self.assertRaises(PublicError) as denied:
            await call_wire(client.wire(), ["guard"], "effect", meta={"verified": "true"})
        self.assertEqual(denied.exception.code, "denied")
        self.assertEqual(effects, [])


if __name__ == "__main__":
    unittest.main()
