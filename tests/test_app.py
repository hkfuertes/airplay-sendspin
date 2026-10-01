from __future__ import annotations

import asyncio
import tempfile
import unittest
from unittest.mock import AsyncMock, Mock, call, patch

from sendspin_bridge.app import AirPlayInput, Config, GroupTarget, Manager, Target
from sendspin_bridge.raop import Receiver, VOLUME
from sendspin_bridge.registry import Group, INBOUND, Registry, Speaker


class FakeTarget:
    def __init__(self, volume: int = 40, connected: bool = True) -> None:
        self.player = object() if connected else None
        self.volume = volume
        self.playing = False
        self.input = Mock()
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
        kitchen.playing = True
        self.assertEqual(manager.set_speaker_volume("kitchen", 73), 73)
        self.assertEqual(kitchen.calls, [73])
        kitchen.input.send_volume.assert_called_once_with(73)
        kitchen.playing = False
        manager.set_speaker_volume("kitchen", 72)
        self.assertEqual(kitchen.calls, [73, 72])
        kitchen.input.send_volume.assert_called_once()
        self.assertEqual(manager.speaker_state("kitchen")["volume"], 72)
        self.assertIsNone(manager.set_speaker_volume("offline", 50))
        self.assertIsNone(manager.set_speaker_volume("missing", 50))

    def test_group_volume_preserves_member_balance_and_skips_offline(self) -> None:
        manager = object.__new__(Manager)
        kitchen, bedroom, offline = FakeTarget(20), FakeTarget(60), FakeTarget(90, connected=False)
        manager.targets = {"kitchen": kitchen, "bedroom": bedroom, "offline": offline}
        group = Mock(active=True)
        manager.group_targets = {"salon": group}

        self.assertEqual(manager.set_group_volume(["kitchen", "bedroom", "offline"], 50, "salon"), {"kitchen": 30, "bedroom": 70})
        self.assertEqual((kitchen.calls, bedroom.calls, offline.calls), ([30], [70], []))
        group.input.send_volume.assert_called_once_with(50)
        for member in (kitchen, bedroom, offline):
            member.input.send_volume.assert_not_called()
        manager.set_group_volume(["kitchen", "bedroom"], 60)  # AirPlay-sent volume must not echo back.
        group.active = False
        manager.set_group_volume(["kitchen", "bedroom"], 65, "salon")
        group.input.send_volume.assert_called_once()
        self.assertEqual(manager.set_group_volume(["missing", "offline"], 50, "salon"), {})

    def test_incoming_airplay_volume_does_not_echo(self) -> None:
        target = Target(Mock(), Speaker(id="kitchen"), [])
        target.input = Mock(events=lambda: [(VOLUME, 0.60)])
        target.handle_events()
        self.assertEqual(target.volume, 60)
        target.input.send_volume.assert_not_called()


class VolumeFeedbackTests(unittest.IsolatedAsyncioTestCase):
    async def test_coalesces_feedback_and_waits_before_receiver_close(self) -> None:
        airplay = AirPlayInput(Mock(), "salon", "Salon", 7030)
        receiver = Mock()
        airplay.receiver = receiver
        started, release = asyncio.Event(), asyncio.Event()

        async def slow_notify(fn, level):
            started.set()
            await release.wait()
            fn(level)

        with patch("sendspin_bridge.app.asyncio.to_thread", side_effect=slow_notify):
            airplay.send_volume(10)
            await started.wait()
            airplay.send_volume(30)
            airplay.send_volume(50)
            release.set()
            await airplay.close()

        receiver.notify_volume.assert_has_calls([call(0.1), call(0.5)])
        self.assertEqual(receiver.notify_volume.call_count, 2)
        receiver.close.assert_called_once()
        self.assertIsNone(airplay.receiver)
        self.assertIsNone(airplay._volume_task)

    async def test_raop_boundary_accepts_normalized_volume(self) -> None:
        receiver = object.__new__(Receiver)
        receiver._lib = Mock()
        receiver._ffi = Mock(NULL=object())
        receiver._receiver = object()
        receiver.notify_volume(0.42)
        receiver._lib.bridge_receiver_notify_volume.assert_called_once_with(receiver._receiver, 0.42)


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
