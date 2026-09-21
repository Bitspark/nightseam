import asyncio
import json
from pathlib import Path
import unittest

from recorded_wire import _Presentation, _RecordedWire, _message, recorded_wire_witness


class RecordedWireTests(unittest.IsolatedAsyncioTestCase):
    async def test_recorded_witness_matches_shared_scenario(self):
        scenario = Path(__file__).resolve().parents[1] / "scenarios/peer/recorded-wire-head-and-order.json"
        expected = json.loads(scenario.read_text(encoding="utf-8"))["steps"][0]["expect"]
        for _ in range(3):
            self.assertEqual(await recorded_wire_witness(5000), expected)
        active = [task for task in asyncio.all_tasks() if task is not asyncio.current_task() and not task.done()]
        self.assertEqual(active, [], "recorded subscribers must stop their writers")

    async def test_witness_deadline_releases_subscriber_tasks(self):
        with self.assertRaises(TimeoutError):
            await recorded_wire_witness(0)
        active = [task for task in asyncio.all_tasks() if task is not asyncio.current_task() and not task.done()]
        self.assertEqual(active, [])

    async def test_recording_delivers_values_from_the_consumer_store(self):
        store = _RecordedWire()
        presentation = _Presentation(store)
        follower = None
        try:
            for value in [29, 13, 41]:
                store.send(["tick"], _message(value))
            head, follower = store.attach(1, presentation.wire, 2, False)
            self.assertEqual(head, 3)
            while await asyncio.wait_for(follower.sent.take(), 1) != 3:
                pass
            store.send(["tick"], _message(7))
            self.assertEqual(await asyncio.wait_for(follower.sent.take(), 1), 4)
            self.assertEqual(await asyncio.wait_for(presentation.collect(), 1), [13, 41, 7])
        finally:
            store.close()
            if follower:
                await follower.done
            presentation.close()


if __name__ == "__main__":
    unittest.main()
