import asyncio
import json
import unittest
from dataclasses import FrozenInstanceError
from pathlib import Path

from nightseam.duplex import (
    MemoryWireLog,
    Message,
    Receiver,
    RecordError,
    RecordOptions,
    ReturnAddress,
    WireError,
    WireRecord,
    at,
    mount,
    record,
)


def message(value):
    return Message({"version": 1, "kind": "event", "data": value})


class Target:
    def __init__(self):
        self.values = asyncio.Queue()
        self.closed = asyncio.Event()
        self.endings = []
        self.refuse = None
        self.receivers = []
        self.on_send = None
        self.on_close = None

    def send(self, path, value):
        if self.refuse:
            raise self.refuse
        if self.closed.is_set():
            raise WireError("closed")
        if self.on_send:
            self.on_send(path, value)
        self.values.put_nowait((tuple(path), value))

    def receive(self, path, receiver):
        entry = (tuple(path), receiver)
        self.receivers.append(entry)

        def detach():
            if entry in self.receivers:
                self.receivers.remove(entry)

        return detach

    def close(self, code=1000, reason=""):
        self.endings.append((code, reason))
        self.closed.set()
        if self.on_close:
            self.on_close()

    async def take(self):
        return await asyncio.wait_for(self.values.get(), 2)


class HeldLog(MemoryWireLog):
    def __init__(self, operation="read"):
        super().__init__()
        self.operation = operation
        self.entered = asyncio.Event()
        self.release = asyncio.Event()
        self.cancelled = asyncio.Event()

    async def hold(self):
        self.entered.set()
        try:
            await self.release.wait()
        except asyncio.CancelledError:
            self.cancelled.set()
            raise

    async def head(self):
        if self.operation == "head":
            await self.hold()
        return await super().head()

    async def append(self, path, value):
        if self.operation == "append":
            await self.hold()
        return await super().append(path, value)

    async def read(self, sequence):
        if self.operation == "read":
            await self.hold()
        return await super().read(sequence)


class RecordTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.tasks_before = asyncio.all_tasks()
        self.owned = []

    async def asyncTearDown(self):
        for wire, ended in self.owned:
            wire.close()
            await asyncio.wait_for(ended.wait(), 2)
        await asyncio.sleep(0)
        remaining = [
            task
            for task in asyncio.all_tasks()
            if task not in self.tasks_before and task is not asyncio.current_task() and not task.done()
        ]
        self.assertEqual(remaining, [], "record/follow left workers running")

    async def recorder(self, target=None, log=None, bound=64, on_close=None):
        ended = asyncio.Event()
        errors = []

        def closed(error):
            errors.append(error)
            if on_close:
                on_close(error)
            ended.set()

        wire = await record(target or Target(), log or MemoryWireLog(), RecordOptions(bound, closed))
        self.owned.append((wire, ended))
        return wire, ended, errors

    async def test_shared_head_table_preserves_exact_replay_then_live_order(self):
        table = Path(__file__).resolve().parents[3] / "conformance/tables/recorded-wire.json"
        for row in json.loads(table.read_text(encoding="utf8"))["cases"]:
            with self.subTest(row=row["name"]):
                log, original, target = HeldLog(), Target(), Target()
                wire, _, _ = await self.recorder(original, log, bound=8)
                source = at(mount({"history": wire}), ["history"])
                for value in row["before"]:
                    source.send(["tick"], message(value))
                follower = await wire.follow(row["after"], target)
                self.assertEqual(follower.head, row["head"])
                await asyncio.wait_for(log.entered.wait(), 2)
                for value in row["during"]:
                    source.send(["tick"], message(value))
                self.assertEqual(await wire.head(), len(row["before"]) + len(row["during"]))
                log.release.set()
                got = []
                for _ in row["expected"]:
                    path, delivered = await target.take()
                    self.assertEqual(path, ("tick",))
                    got.append(delivered.frame["data"])
                self.assertEqual(got, row["expected"])
                wire.send(["fence"], message(99))
                self.assertEqual(await target.take(), (("fence",), message(99)))
                follower.close()
                await follower.wait_closed()
                self.assertEqual(len(target.endings), 1)

    async def test_append_precedes_forward_and_head_fences_admitted_appends(self):
        log, target = HeldLog("append"), Target()
        wire, _, _ = await self.recorder(target, log)
        wire.send(["tick"], message(1))
        self.assertFalse(log.entered.is_set(), "storage ran on send's stack")
        self.assertTrue(target.values.empty())
        await asyncio.wait_for(log.entered.wait(), 2)
        fence = asyncio.create_task(wire.head())
        await asyncio.sleep(0)
        self.assertFalse(fence.done())
        self.assertTrue(target.values.empty())
        log.release.set()
        self.assertEqual(await fence, 1)
        self.assertEqual((await log.read(1)).message.frame["data"], 1)
        self.assertEqual(await target.take(), (("tick",), message(1)))

    async def test_writer_bound_counts_waiting_data_and_controls(self):
        for control in ("head", "follow"):
            with self.subTest(control=control):
                log, target, subscriber = HeldLog("append"), Target(), Target()
                wire, ended, errors = await self.recorder(target, log, bound=2)
                wire.send([], message(1))
                await asyncio.wait_for(log.entered.wait(), 2)
                waiting = asyncio.create_task(wire.head() if control == "head" else wire.follow(0, subscriber))
                await asyncio.sleep(0)
                wire.send([], message(2))
                with self.assertRaises(RecordError) as caught:
                    wire.send([], message(3))
                self.assertEqual(caught.exception.code, "overflow")
                self.assertFalse(target.closed.is_set(), "carrier closed on send's stack")
                with self.assertRaises(WireError):
                    await waiting
                await asyncio.wait_for(ended.wait(), 2)
                self.assertIs(errors[0], caught.exception)
                self.assertTrue(log.cancelled.is_set())
                self.assertEqual(target.endings[0][0], 1008)
                self.assertFalse(subscriber.closed.is_set(), "pending attachment owned its target")

    async def test_head_and_follow_overflow_refuse_and_end_the_recorder(self):
        for control in ("head", "follow"):
            log = HeldLog("append")
            wire, ended, _ = await self.recorder(log=log, bound=1)
            wire.send([], message(1))
            await asyncio.wait_for(log.entered.wait(), 2)
            wire.send([], message(2))
            with self.assertRaises(RecordError) as caught:
                await (wire.head() if control == "head" else wire.follow(0, Target()))
            self.assertEqual(caught.exception.code, "overflow")
            await asyncio.wait_for(ended.wait(), 2)

    async def test_stalled_handoff_closes_its_mount_but_not_healthy_work(self):
        log, original, slow_root, healthy = HeldLog(), Target(), Target(), Target()
        wire, _, _ = await self.recorder(original, log, bound=2)
        wire.send(["tick"], message(1))
        slow_view = at(mount({"slow": slow_root}), ["slow"])
        closes = []
        slow_view.receive([], Receiver(closed=lambda *ending: closes.append(ending)))
        slow = await wire.follow(0, slow_view)
        await asyncio.wait_for(log.entered.wait(), 2)
        fast = await wire.follow(1, healthy)
        for value in range(2, 5):
            wire.send(["tick"], message(value))
            self.assertEqual((await healthy.take())[1].frame["data"], value)
        await slow.wait_closed()
        self.assertIsInstance(slow.error, RecordError)
        self.assertEqual(slow.error.code, "overflow")
        self.assertEqual(len(closes), 1)
        self.assertEqual(closes[0][0], 1008)
        self.assertFalse(slow_root.closed.is_set())
        self.assertTrue(log.cancelled.is_set())
        slow_root.send(["probe"], message(99))
        wire.send(["tick"], message(5))
        self.assertEqual((await healthy.take())[1].frame["data"], 5)
        self.assertEqual(await wire.head(), 5)
        fast.close()
        await fast.wait_closed()

    async def test_recorder_failures_preserve_errors_and_allow_reentrant_close(self):
        for mode in ("append", "sequence", "target"):
            sentinel = RuntimeError(mode)

            class BrokenLog(MemoryWireLog):
                async def append(self, path, value):
                    if mode == "append":
                        raise sentinel
                    sequence = await super().append(path, value)
                    return sequence + (mode == "sequence")

            target = Target()
            if mode == "target":
                target.refuse = sentinel
            observed = []

            def closed(error):
                wire.close()
                observed.append(error)

            wire, ended, errors = await self.recorder(target, BrokenLog(), on_close=closed)
            follower_target = Target()
            follower = await wire.follow(0, follower_target)
            wire.send([], message(1))
            await asyncio.wait_for(ended.wait(), 2)
            await follower.wait_closed()
            if mode == "sequence":
                self.assertIsInstance(errors[0], RecordError)
                self.assertEqual(errors[0].code, "sequence")
            else:
                self.assertIs(errors[0], sentinel)
            self.assertEqual(observed, errors)
            self.assertIs(follower.error, errors[0])
            self.assertEqual(target.endings[0][0], 1011)
            self.assertEqual(follower_target.endings[0][0], 1011)
            self.assertEqual(len(target.endings), 1)
            with self.assertRaises(WireError):
                await wire.head()

    async def test_follower_failures_leave_recorder_and_other_followers_usable(self):
        for mode in ("read", "sequence", "target"):
            sentinel = RuntimeError(mode)

            class BrokenLog(MemoryWireLog):
                async def read(self, sequence):
                    if mode == "read":
                        raise sentinel
                    entry = await super().read(sequence)
                    return WireRecord(sequence + (mode == "sequence"), entry.path, entry.message)

            log, target, healthy = BrokenLog(), Target(), Target()
            await log.append([], message(1))
            if mode == "target":
                target.refuse = sentinel
            wire, _, _ = await self.recorder(log=log)
            fast = await wire.follow(1, healthy)
            failed = await wire.follow(0, target)
            await failed.wait_closed()
            if mode == "sequence":
                self.assertEqual(failed.error.code, "sequence")
            else:
                self.assertIs(failed.error, sentinel)
            self.assertEqual(target.endings[0][0], 1011)
            wire.send([], message(2))
            self.assertEqual(await wire.head(), 2)
            self.assertEqual((await healthy.take())[1].frame["data"], 2)
            fast.close()
            await fast.wait_closed()

    async def test_cancelled_setup_never_owns_target(self):
        target, log = Target(), HeldLog("head")
        setup = asyncio.create_task(record(target, log))
        await asyncio.wait_for(log.entered.wait(), 2)
        setup.cancel()
        with self.assertRaises(asyncio.CancelledError):
            await setup
        self.assertTrue(log.cancelled.is_set())
        self.assertFalse(target.closed.is_set())
        target.send([], message(1))

    async def test_cancelled_controls_preserve_admitted_appends(self):
        log, subscriber = HeldLog("append"), Target()
        wire, _, _ = await self.recorder(log=log)
        wire.send([], message(1))
        await asyncio.wait_for(log.entered.wait(), 2)
        head = asyncio.create_task(wire.head())
        follow = asyncio.create_task(wire.follow(0, subscriber))
        await asyncio.sleep(0)
        head.cancel()
        follow.cancel()
        for task in (head, follow):
            with self.assertRaises(asyncio.CancelledError):
                await task
        log.release.set()
        self.assertEqual(await wire.head(), 1)
        self.assertFalse(subscriber.closed.is_set())
        self.assertFalse(log.cancelled.is_set())

    async def test_attachment_completed_during_cancellation_is_cleaned_up(self):
        log, subscriber = HeldLog("append"), Target()
        wire, _, _ = await self.recorder(log=log)
        wire.send([], message(1))
        await asyncio.wait_for(log.entered.wait(), 2)
        pending = asyncio.create_task(wire.follow(0, subscriber))
        await asyncio.sleep(0)
        log.release.set()
        # The writer attaches first; cancellation runs before follow's waiter.
        asyncio.get_running_loop().call_soon(pending.cancel)
        with self.assertRaises(asyncio.CancelledError):
            await pending
        await asyncio.wait_for(subscriber.closed.wait(), 2)
        self.assertEqual(len(subscriber.endings), 1)
        self.assertEqual(await wire.head(), 1)

    async def test_context_cancellation_closes_and_joins_blocked_replay(self):
        log, subscriber = HeldLog(), Target()
        await log.append([], message(1))
        wire, _, _ = await self.recorder(log=log)
        owned = asyncio.Event()
        followers = []

        async def consume():
            async with await wire.follow(0, subscriber) as follower:
                followers.append(follower)
                owned.set()
                await asyncio.Event().wait()

        task = asyncio.create_task(consume())
        await asyncio.wait_for(owned.wait(), 2)
        await asyncio.wait_for(log.entered.wait(), 2)
        task.cancel()
        with self.assertRaises(asyncio.CancelledError):
            await task
        self.assertTrue(log.cancelled.is_set())
        self.assertTrue(subscriber.closed.is_set())
        self.assertIsInstance(followers[0].error, asyncio.CancelledError)
        self.assertEqual(subscriber.endings[0][0], 1000)
        self.assertEqual(await wire.head(), 1)

    async def test_cancelled_completion_wait_does_not_cancel_cleanup(self):
        log, subscriber = HeldLog(), Target()
        await log.append([], message(1))
        wire, _, _ = await self.recorder(log=log)
        follower = await wire.follow(0, subscriber)
        await asyncio.wait_for(log.entered.wait(), 2)
        wait = asyncio.create_task(follower.wait_closed())
        await asyncio.sleep(0)
        wait.cancel()
        with self.assertRaises(asyncio.CancelledError):
            await wait
        self.assertFalse(subscriber.closed.is_set())
        follower.close()
        follower.close()
        await follower.wait_closed()
        await follower.wait_closed()
        self.assertIsNone(follower.error)
        self.assertTrue(log.cancelled.is_set())
        self.assertEqual(len(subscriber.endings), 1)

    async def test_seeded_log_capability_identity_path_copy_and_receiver_delegation(self):
        target, subscriber, log = Target(), Target(), MemoryWireLog()
        address = ReturnAddress(target)
        opaque = object()
        value = Message({"version": 1, "kind": "request", "id": "c:1", "params": opaque}, address)
        path = ["", "é"]
        await log.append(path, value)
        path[:] = ["mutated"]
        entry = await log.read(1)
        self.assertEqual(tuple(entry.path), ("", "é"))
        self.assertIs(entry.message, value)
        with self.assertRaises(FrozenInstanceError):
            entry.sequence = 2
        wire, _, _ = await self.recorder(target, log)
        self.assertEqual(await wire.head(), 1)
        receiver = Receiver(namespace=True)
        detach = wire.receive(["reply"], receiver)
        self.assertEqual(target.receivers, [(("reply",), receiver)])
        detach()
        detach()
        self.assertEqual(target.receivers, [])
        follower = await wire.follow(0, subscriber)
        got_path, got = await subscriber.take()
        self.assertEqual(got_path, ("", "é"))
        self.assertIs(got, value)
        self.assertIs(got.return_address, address)
        self.assertIs(got.frame["params"], opaque)
        with self.assertRaises(AttributeError):
            follower.head = 9
        with self.assertRaises(AttributeError):
            follower.error = RuntimeError()
        route = ["original"]
        wire.send(route, value)
        route[:] = ["changed"]
        self.assertEqual(await wire.head(), 2)
        self.assertEqual((await log.read(2)).path, ("original",))
        self.assertIs((await subscriber.take())[1], value)
        follower.close()
        await follower.wait_closed()

    async def test_sequence_validation_and_bad_setup_do_not_take_ownership(self):
        for invalid in (-1, True, 1.0, 2**53, "1"):
            target = Target()

            class BadHead(MemoryWireLog):
                async def head(self):
                    return invalid

            with self.assertRaises(RecordError):
                await record(target, BadHead())
            self.assertFalse(target.closed.is_set())
        wire, _, _ = await self.recorder()
        for invalid in (-1, True, 0.5, 2**53, 1, "0"):
            target = Target()
            with self.assertRaises(RecordError):
                await wire.follow(invalid, target)
            self.assertFalse(target.closed.is_set())
        log = MemoryWireLog()
        for invalid in (0, -1, True, 1.0, 1, 2**53):
            with self.assertRaises(RecordError):
                await log.read(invalid)
        for invalid in (0, -1, True, 1.5, 2**53):
            with self.assertRaises(ValueError):
                await record(Target(), log, RecordOptions(invalid))
        with self.assertRaises(WireError) as caught:
            wire.send(["\ud800"], message(1))
        self.assertEqual(caught.exception.code, "invalid_path")
        self.assertEqual(await wire.head(), 0)

    async def test_append_results_must_be_contiguous_safe_integers(self):
        for invalid in (0, -1, True, 1.0, 2, 2**53):

            class BadAppend(MemoryWireLog):
                async def append(self, path, value):
                    return invalid

            wire, ended, errors = await self.recorder(log=BadAppend())
            wire.send([], message(1))
            await asyncio.wait_for(ended.wait(), 2)
            self.assertEqual(errors[0].code, "sequence")

    async def test_callbacks_stay_off_send_stack_with_eager_tasks(self):
        factories = [None]
        if hasattr(asyncio, "eager_task_factory"):
            factories.append(asyncio.eager_task_factory)
        loop = asyncio.get_running_loop()
        original_factory = loop.get_task_factory()
        try:
            for factory in factories:
                loop.set_task_factory(factory)
                sending = False
                calls = []

                class CheckingLog(MemoryWireLog):
                    async def append(self, path, value):
                        self_test.assertFalse(sending)
                        calls.append("append")
                        return await super().append(path, value)

                self_test = self
                target = Target()
                target.on_send = lambda *args: self.assertFalse(sending)
                target.on_close = lambda: self.assertFalse(sending)
                wire, ended, _ = await self.recorder(
                    target, CheckingLog(), bound=1, on_close=lambda error: self.assertFalse(sending)
                )
                sending = True
                wire.send([], message(1))
                self.assertEqual(calls, [])
                sending = False
                await target.take()
                self.assertEqual(await wire.head(), 1)
                sending = True
                wire.send([], message(2))
                with self.assertRaises(RecordError):
                    wire.send([], message(3))
                sending = False
                await asyncio.wait_for(ended.wait(), 2)
        finally:
            loop.set_task_factory(original_factory)

    async def test_recorder_close_joins_a_follower_already_cleaning_up(self):
        cleaning = asyncio.Event()
        finish = asyncio.Event()

        class CleaningLog(HeldLog):
            async def hold(self):
                self.entered.set()
                try:
                    await asyncio.Event().wait()
                finally:
                    cleaning.set()
                    await finish.wait()

        log, target = CleaningLog(), Target()
        await log.append([], message(1))
        wire, ended, _ = await self.recorder(log=log)
        follower = await wire.follow(0, target)
        await asyncio.wait_for(log.entered.wait(), 2)
        follower.close()
        await asyncio.wait_for(cleaning.wait(), 2)
        wire.close()
        wait = asyncio.create_task(follower.wait_closed())
        await asyncio.sleep(0)
        self.assertFalse(ended.is_set())
        self.assertFalse(wait.done())
        wait.cancel()
        with self.assertRaises(asyncio.CancelledError):
            await wait
        finish.set()
        await asyncio.wait_for(ended.wait(), 2)
        await follower.wait_closed()
        self.assertEqual(len(target.endings), 1)

    async def test_every_profile_frame_replays_as_the_same_opaque_message(self):
        address = ReturnAddress(Target())
        frames = [
            {"version": 1, "kind": "request", "id": "c:1", "params": {"nested": [1]}},
            {"version": 1, "kind": "response", "id": "c:1", "result": {"scope": "original"}},
            {"version": 1, "kind": "event", "data": None},
            {"version": 1, "kind": "cancel", "id": "c:1"},
        ]
        wire, ended, _ = await self.recorder()
        values = [Message(frame, address) for frame in frames]
        for value in values:
            wire.send(["opaque"], value)
        target = Target()
        follower = await wire.follow(0, target)
        for value in values:
            _, actual = await target.take()
            self.assertIs(actual, value)
            self.assertIs(actual.frame, value.frame)
            self.assertIs(actual.return_address, address)
        wire.close(4007, "finished")
        await asyncio.wait_for(ended.wait(), 2)
        await follower.wait_closed()
        self.assertEqual(target.endings, [(4007, "finished")])
        with self.assertRaises(WireError):
            wire.receive([], Receiver())

    async def test_reentrant_follower_close_does_not_start_another_replay_read(self):
        reads = []

        class CheckedLog(MemoryWireLog):
            async def read(self, sequence):
                reads.append(sequence)
                return await super().read(sequence)

        log, target = CheckedLog(), Target()
        await log.append([], message(1))
        await log.append([], message(2))
        wire, _, _ = await self.recorder(log=log)
        follower = await wire.follow(0, target)
        target.on_send = lambda *args: follower.close()
        await asyncio.wait_for(follower.wait_closed(), 2)
        self.assertEqual(reads, [1])
        self.assertEqual(target.values.qsize(), 1)
        self.assertIsNone(follower.error)
        self.assertEqual(await wire.head(), 2)


if __name__ == "__main__":
    unittest.main()
