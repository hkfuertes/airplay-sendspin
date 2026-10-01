from __future__ import annotations

import unittest

from airplay_sendspin.app import Manager, Target
from airplay_sendspin.registry import Speaker


class FakeTarget:
    def __init__(self, volume: int = 40, connected: bool = True) -> None:
        self.player = object() if connected else None
        self.volume = volume
        self.calls: list[int] = []

    def set_volume(self, volume: int) -> None:
        self.calls.append(volume)
        self.volume = volume


class ManagerVolumeTests(unittest.TestCase):
    def test_only_exposed_speakers_get_an_input(self) -> None:
        manager = object.__new__(Manager)
        self.assertIsNone(Target(manager, Speaker(id="kitchen", exposed=False), []).input)
        self.assertIsNotNone(Target(manager, Speaker(id="bedroom", exposed=True), []).input)

    def test_individual_volume_is_live_only(self) -> None:
        manager = object.__new__(Manager)
        kitchen = FakeTarget()
        offline = FakeTarget(connected=False)
        manager.targets = {"kitchen": kitchen, "offline": offline}

        self.assertEqual(manager.speaker_state("kitchen"), {"connected": True, "volume": 40})
        self.assertEqual(manager.set_speaker_volume("kitchen", 73), 73)
        self.assertEqual(kitchen.calls, [73])
        self.assertEqual(manager.speaker_state("kitchen")["volume"], 73)
        self.assertIsNone(manager.set_speaker_volume("offline", 50))
        self.assertIsNone(manager.set_speaker_volume("missing", 50))


if __name__ == "__main__":
    unittest.main()
