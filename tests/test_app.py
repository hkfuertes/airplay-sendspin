from __future__ import annotations

import asyncio
import tempfile
import unittest
from unittest.mock import AsyncMock, Mock, call, patch

import numpy as np

from sendspin_bridge.app import AirPlayInput, Config, GroupTarget, Manager, Target
from sendspin_bridge.raop import Receiver, VOLUME
from sendspin_bridge.audio import CHUNK_BYTES, CHUNK_SAMPLES, GroupBuffer
from sendspin_bridge.registry import Group, INBOUND, Registry, Speaker, Stereo


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
    def test_only_unpaired_exposed_speakers_get_an_input(self) -> None:
        manager = object.__new__(Manager)
        self.assertIsNone(Target(manager, Speaker(id="kitchen", exposed=False), []).input)
        self.assertIsNotNone(Target(manager, Speaker(id="bedroom", exposed=True), []).input)
        speaker = Speaker(id="left", exposed=True)
        self.assertIsNone(Target(manager, speaker, [], paired=True).input)
        self.assertTrue(speaker.exposed)  # Unpairing restores the saved preference.

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

    def test_stereo_volume_notifies_only_its_active_airplay_target(self) -> None:
        manager = object.__new__(Manager)
        left, right = FakeTarget(20), FakeTarget(60)
        manager.targets = {"left": left, "right": right}
        stereo = Mock(active=True)
        manager.stereo_targets = {"pair": stereo}
        self.assertEqual(manager.set_stereo_volume(["left", "right"], 50, "pair"), {"left": 30, "right": 70})
        stereo.input.send_volume.assert_called_once_with(50)
        left.input.send_volume.assert_not_called()
        right.input.send_volume.assert_not_called()

    def test_incoming_airplay_volume_does_not_echo(self) -> None:
        target = Target(Mock(), Speaker(id="kitchen"), [])
        target.input = Mock(events=lambda: [(VOLUME, 0.60)])
        target.handle_events()
        self.assertEqual(target.volume, 60)
        target.input.send_volume.assert_not_called()


class StereoRoutingTests(unittest.TestCase):
    def test_stereo_pair_and_multiroom_share_timestamp_but_not_channels(self) -> None:
        manager = object.__new__(Manager)
        manager.targets = {}
        pair = Stereo("pair", "left", "right")
        group = GroupTarget(manager, Group("home", speaker_ids=["left", "right", "kitchen"]), [pair])
        group.active = True
        audio = np.empty(CHUNK_SAMPLES, dtype="<i2")
        audio[::2], audio[1::2] = 100, 300
        group.buffer = GroupBuffer(lambda _frames: audio.tobytes())
        for speaker_id in ("left", "right", "kitchen"):
            target = Target(manager, Speaker(id=speaker_id, exposed=False), [group])
            target.player = object()
            manager.targets[speaker_id] = target
        for speaker_id, left, right in (("left", 100, 100), ("right", 300, 300), ("kitchen", 100, 300)):
            with self.subTest(speaker=speaker_id):
                result = np.frombuffer(manager.targets[speaker_id].render(0), dtype="<i2")
                self.assertTrue(np.all(result[::2] == left))
                self.assertTrue(np.all(result[1::2] == right))
        stereo = GroupTarget(manager, Group("pair", speaker_ids=["left", "right"]), [pair], key="stereo:pair")
        stereo.active = True
        stereo.started_at = 2
        group.started_at = 1
        stereo.buffer = GroupBuffer(lambda _frames: audio.tobytes())
        manager.targets["left"].groups.append(stereo)
        manager.targets["right"].groups.append(stereo)
        solo = np.empty(CHUNK_SAMPLES, dtype="<i2")
        solo[::2], solo[1::2] = 50, 150
        manager.targets["left"].playing = True
        manager.targets["left"].started_at = 3
        manager.targets["left"].input = Mock(read_pcm=lambda: solo.tobytes())
        selected = np.frombuffer(manager.targets["left"].render(0), dtype="<i2")
        self.assertTrue(np.all(selected[::2] == 50))  # Latest individual target wins.
        self.assertTrue(np.all(selected[1::2] == 150))
        manager.targets["left"].playing = False
        manager.targets["right"].player = None
        fallback = np.frombuffer(manager.targets["left"].render(0), dtype="<i2")
        self.assertTrue(np.all(fallback[::2] == 100))  # Stereo is next newest; disconnected partner gets full mix.
        self.assertTrue(np.all(fallback[1::2] == 300))


class SpotifyRoutingTests(unittest.TestCase):
    def test_spotify_and_airplay_stay_active_on_the_same_pair_and_group(self) -> None:
        manager = object.__new__(Manager)
        manager.spotify_enabled = True
        manager.config = Config()
        manager.registry = Registry()
        manager.address = "127.0.0.1"
        manager.targets = {}
        pair = Stereo("pair", "left", "right")
        group = GroupTarget(manager, Group("home", speaker_ids=["left", "right", "other"]), [pair])
        group.active = True
        group.started_at = 1
        assert group.spotify is not None
        group.spotify.playing = True
        group.spotify.started_at = 2
        airplay = np.empty(CHUNK_SAMPLES, dtype="<i2")
        airplay[::2], airplay[1::2] = 100, 200
        spotify = np.empty(CHUNK_SAMPLES, dtype="<i2")
        spotify[::2], spotify[1::2] = 300, 400
        group.buffer = GroupBuffer(lambda _: airplay.tobytes())
        group.spotify_buffer = GroupBuffer(lambda _: spotify.tobytes())
        for speaker_id in ("left", "right", "other"):
            target = Target(manager, Speaker(id=speaker_id, exposed=False), [group])
            target.player = object()
            manager.targets[speaker_id] = target
        for speaker_id, left, right in (("left", 300, 300), ("right", 400, 400), ("other", 300, 400)):
            with self.subTest(speaker=speaker_id):
                result = np.frombuffer(manager.targets[speaker_id].render(0), dtype="<i2")
                self.assertTrue(np.all(result[::2] == left))
                self.assertTrue(np.all(result[1::2] == right))
        group.spotify.playing = False
        self.assertTrue(manager.targets["left"].active)
        fallback = np.frombuffer(manager.targets["left"].render(1), dtype="<i2")
        self.assertTrue(np.all(fallback == 100))
        group.active = False
        self.assertFalse(manager.targets["left"].active)

    def test_individual_exposure_applies_to_both_inputs(self) -> None:
        manager = object.__new__(Manager)
        manager.spotify_enabled = True
        manager.config = Config()
        manager.registry = Registry()
        manager.address = "127.0.0.1"
        exposed = Target(manager, Speaker(id="solo"), [])
        self.assertIsNotNone(exposed.spotify)
        self.assertIsNone(Target(manager, Speaker(id="hidden", exposed=False), []).spotify)
        self.assertIsNone(Target(manager, Speaker(id="left"), [], paired=True).spotify)
        assert exposed.spotify is not None
        exposed.spotify.playing = True
        exposed.spotify._pcm.extend(np.full(CHUNK_SAMPLES, 250, dtype="<i2").tobytes())
        self.assertTrue(exposed.active)
        self.assertTrue(np.all(np.frombuffer(exposed.render(0), dtype="<i2") == 250))
        exposed.input = Mock(read_pcm=lambda: np.full(CHUNK_SAMPLES, 100, dtype="<i2").tobytes())
        exposed.playing = True
        exposed.started_at = 10
        exposed.spotify._pcm.extend(np.full(CHUNK_SAMPLES, 250, dtype="<i2").tobytes())
        self.assertTrue(np.all(np.frombuffer(exposed.render(1), dtype="<i2") == 100))
        self.assertEqual(len(exposed.spotify._pcm), 0)  # Losing pipe still drains.
        exposed.playing = False
        exposed.spotify._pcm.extend(np.full(CHUNK_SAMPLES, 250, dtype="<i2").tobytes())
        self.assertTrue(np.all(np.frombuffer(exposed.render(2), dtype="<i2") == 250))


class SpotifyNameTests(unittest.TestCase):
    def test_names_are_unique_stable_and_fit_one_mdns_label(self) -> None:
        from sendspin_bridge.app import _spotify_names

        long = "Ñ" * 40  # 80 UTF-8 bytes.
        registry = Registry(
            speakers=[Speaker(id="a", exposed_name="Echo"), Speaker(id="b", exposed_name="Echo"),
                      Speaker(id="hidden", exposed_name="Echo", exposed=False), Speaker(id="l", exposed_name="L"),
                      Speaker(id="r", exposed_name="R"), Speaker(id="long", exposed_name=long)],
            stereos=[Stereo("pair", "l", "r", exposed_name="Echo")],
        )
        names = _spotify_names(registry)
        suffix = registry.exposed_suffix
        self.assertEqual(names["stereo:pair"], "Echo" + suffix)
        self.assertEqual(names["a"], "Echo" + suffix + " 2")
        self.assertEqual(names["b"], "Echo" + suffix + " 3")
        self.assertNotIn("hidden", names)
        self.assertNotIn("l", names)
        self.assertLessEqual(len(names["long"].encode()), 62)
        self.assertTrue(names["long"].startswith("Ñ"))
        self.assertEqual(names, _spotify_names(registry))


class SpotifyNoSpeakerTests(unittest.IsolatedAsyncioTestCase):
    async def test_undiscovered_group_does_not_block_spotify_pipe(self) -> None:
        manager = object.__new__(Manager)
        manager.spotify_enabled = True
        manager.config = Config()
        manager.registry = Registry()
        manager.address = "127.0.0.1"
        manager.targets = {}
        group = GroupTarget(manager, Group("home", speaker_ids=["missing"]))
        assert group.spotify is not None
        group.spotify.playing = True
        group.spotify._pcm.extend(b"\x01" * (CHUNK_BYTES * 10))
        manager.group_targets = {"home": group}
        manager.stereo_targets = {}
        manager.server = Mock(clock=Mock(now_us=lambda: 1000000))
        manager.next_play_start_us = None
        manager.playback_chunk = 0
        task = asyncio.create_task(manager._pump_audio())
        try:
            await asyncio.sleep(0.06)
        finally:
            task.cancel()
            await asyncio.gather(task, return_exceptions=True)
        self.assertLess(len(group.spotify._pcm), CHUNK_BYTES * 10)


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
    async def test_unpairing_restores_only_previously_exposed_individual_input(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            manager = Manager(Config(config_path=f"{temp}/config.xml"))
            left, right = Speaker(id="left", exposed=True), Speaker(id="right", exposed=False)
            manager.registry = Registry(speakers=[left, right], stereos=[Stereo("pair", "left", "right")])
            with patch.object(AirPlayInput, "start", new_callable=AsyncMock):
                paired = await manager._ensure_target(left)
                self.assertIsNone(paired.input)
                await paired.close()
                manager.targets.clear()
                manager.registry = Registry(speakers=[left, right])
                unpaired = await manager._ensure_target(left)
                hidden = await manager._ensure_target(right)
                self.assertIsNotNone(unpaired.input)
                self.assertIsNone(hidden.input)
                await unpaired.close()
                await hidden.close()

    async def test_early_inbound_player_joins_stereo_and_multiroom(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            manager = Manager(Config(config_path=f"{temp}/config.xml"))
            speaker = Speaker(id="left", direction=INBOUND, exposed=True)
            other = Speaker(id="right", direction=INBOUND, exposed=False)
            manager.registry = Registry(
                speakers=[speaker, other], stereos=[Stereo("pair", "left", "right")],
                groups=[Group("home", speaker_ids=["left", "right"])],
            )

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
                    manager.stereo_targets["pair"].active = True
                    self.assertTrue(manager.targets[speaker.id].active)
                    self.assertIsNone(manager.targets[speaker.id].input)
                    self.assertEqual(len(manager.targets[speaker.id].groups), 2)
                finally:
                    await manager.close()

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
