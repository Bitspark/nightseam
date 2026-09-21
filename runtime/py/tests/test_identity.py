import unittest

from nightseam.duplex import pipe
from nightseam.runtime import IDENTITY_METHOD, Peer, PublicError, check_identity, identity_handler


class IdentityTests(unittest.IsolatedAsyncioTestCase):
    async def test_identity_refusal_and_absence_leave_carrier_usable(self):
        a, b = pipe()
        client, server = Peer(a), Peer(b, "server")
        self.addAsyncCleanup(client.close)
        self.addAsyncCleanup(server.close)
        identity = {"path": "worker", "digest": "a" * 64}
        await check_identity(client.call, identity)
        server.handle(IDENTITY_METHOD, identity_handler(identity))
        identity["path"] = "mutated"
        await check_identity(client.call, {"path": "worker", "digest": "a" * 64})
        await check_identity(client.call, {"path": "worker"})
        for expected in ({"path": "other"}, {"path": "worker", "digest": "b" * 64}):
            with self.assertRaises(PublicError) as error:
                await check_identity(client.call, expected)
            self.assertEqual(error.exception.code, "contract_mismatch")
        with self.assertRaises(PublicError) as error:
            await client.call(IDENTITY_METHOD, {"path": "worker", "digest": ""})
        self.assertEqual(error.exception.code, "contract_invalid")
        server.handle("echo", lambda value, context: value)
        self.assertEqual(await client.call("echo", 42), 42)

    async def test_only_missing_method_means_absent_identity(self):
        async def malformed(*args, **kwargs):
            return {"path": "worker", "digest": None}

        with self.assertRaises(PublicError) as error:
            await check_identity(malformed, {"path": "worker"})
        self.assertEqual(error.exception.code, "contract_invalid")

        async def refuses(*args, **kwargs):
            raise PublicError("busy", "busy")

        with self.assertRaises(PublicError) as error:
            await check_identity(refuses, {"path": "worker"})
        self.assertEqual(error.exception.code, "busy")

    async def test_check_passes_a_bounded_deadline(self):
        async def call(method, value, **options):
            self.assertEqual(method, IDENTITY_METHOD)
            self.assertEqual(options["timeout_ms"], 30_000)
            return value

        await check_identity(call, {"path": "worker"})

    def test_identity_rejects_non_scalar_paths_and_malformed_members(self):
        for identity in (
            {},
            [],
            {"path": ""},
            {"path": "\ud800"},
            {"path": "worker", "extra": 1},
            {"path": "worker", "digest": "A" * 64},
        ):
            with self.subTest(identity=identity), self.assertRaises(PublicError) as error:
                identity_handler(identity)
            self.assertEqual(error.exception.code, "contract_invalid")


if __name__ == "__main__":
    unittest.main()
