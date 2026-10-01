from __future__ import annotations

import tempfile
import unittest
from unittest.mock import AsyncMock, Mock, patch

from sendspin_bridge.app import Config, GroupTarget, Manager, Target
from sendspin_bridge.registry import Group, INBOUND, Registry, Speaker


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

    def test_group_volume_preserves_member_balance_and_skips_offline(self) -> None:
        manager = object.__new__(Manager)
        kitchen, bedroom, offline = FakeTarget(20), FakeTarget(60), FakeTarget(90, connected=False)
        manager.targets = {"kitchen": kitchen, "bedroom": bedroom, "offline": offline}

        self.assertEqual(manager.set_group_volume(["kitchen", "bedroom", "offline"], 50), {"kitchen": 30, "bedroom": 70})
        self.assertEqual((kitchen.calls, bedroom.calls, offline.calls), ([30], [70], []))
        self.assertEqual(manager.set_group_volume(["missing", "offline"], 50), {})


class ManagerStartupTests(unittest.IsolatedAsyncioTestCase):
    async def test_early_inbound_player_joins_group(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            manager = Manager(Config(config_path=f"{temp}/config.xml"))
            speaker = Speaker(id="echo-show", direction=INBOUND, exposed=False)
            manager.registry = Registry(speakers=[speaker], groups=[Group(id="salon", speaker_ids=[speaker.id])])

            async def connect_early(**_):
                await manager._ensure_target(speaker)

            server = Mock(start_server=AsyncMock(side_effect=connect_early), close=AsyncMock())
            server.add_event_listener.return_value = lambda: None
            web = Mock(start=AsyncMock(), close=AsyncMock(), url="http://localhost")
            with (
                patch("sendspin_bridge.app._load_identity"),
                patch("sendspin_bridge.app.FileServerPairingStore.open", new_callable=AsyncMock),
                patch("sendspin_bridge.app.lan_ipv4", return_value="127.0.0.1"),
                patch("sendspin_bridge.app.SendspinServer", return_value=server),
                patch("sendspin_bridge.app.ConfigWeb", return_value=web),
                patch.object(GroupTarget, "start", new_callable=AsyncMock),
            ):
                try:
                    await manager.start()
                    manager.group_targets["salon"].active = True
                    self.assertTrue(manager.targets[speaker.id].active)
                finally:
                    await manager.close()


if __name__ == "__main__":
    unittest.main()
