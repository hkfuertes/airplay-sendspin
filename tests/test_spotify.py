from __future__ import annotations

import asyncio
import hashlib
import json
import os
import tempfile
import unittest
import urllib.request
from pathlib import Path
from unittest.mock import AsyncMock, Mock, patch

import numpy as np

from sendspin_bridge.audio import CHUNK_BYTES, CHUNK_FRAMES, CHUNK_SAMPLES
from sendspin_bridge.spotify import LIBRARY, MAX_PCM_BYTES, SPOTIFY_TYPE, SpotifyInput


def _advertiser_factory(advertisers: list[Mock]):
    def advertise(*args):
        advertisers.append(Mock(args=args, start=AsyncMock(), close=AsyncMock()))
        return advertisers[-1]

    return advertise


class SpotifyTests(unittest.IsolatedAsyncioTestCase):
    async def test_events_keep_audio_across_pauses_and_gapless_track_changes(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            source = SpotifyInput("solo", "Solo", Path(temp), "127.0.0.1", on_reset=Mock())
            source._event_fd, write_fd = os.pipe()
            os.set_blocking(source._event_fd, False)
            chunk = b"\x01\x00\x02\x00" * CHUNK_FRAMES
            try:
                def send(data: bytes) -> None:
                    os.write(write_fd, data)
                    source._read_events()

                send(b"playing\n")
                started = source.started_at
                self.assertTrue(source.playing and started)
                source._pcm.extend(chunk)
                send(b"not_playing\nwill_play\npla")  # A track change, split across reads.
                send(b"ying\n")
                self.assertEqual((source.playing, source.started_at, len(source._pcm)), (True, started, CHUNK_BYTES))
                send(b"paused\n")
                self.assertFalse(source.playing)
                self.assertEqual(len(source._pcm), CHUNK_BYTES)  # Resume continues where it paused.
                send(b"playing\n")
                self.assertTrue(source.playing)
                self.assertGreater(source.started_at, started)  # Resuming is a start: it wins the speaker.
                self.assertEqual(source.read_pcm(), chunk)
                source.on_reset.assert_not_called()
                source._pcm.extend(chunk)
                send(b"seek\n")
                self.assertEqual((source.playing, bytes(source._pcm)), (True, b""))
                source._pcm.extend(chunk)
                send(b"stopped\n")
                self.assertEqual((source.playing, bytes(source._pcm), source._discard), (False, b"", True))
                self.assertEqual(source.on_reset.call_count, 2)
            finally:
                os.close(source._event_fd)
                os.close(write_fd)
                source._event_fd = None

    async def test_registration_publishes_the_pairing_port_with_python_zeroconf(self) -> None:
        advertisers: list[Mock] = []
        with tempfile.TemporaryDirectory() as temp, \
                patch("sendspin_bridge.spotify.Advertiser", side_effect=_advertiser_factory(advertisers)):
            source = SpotifyInput("group:home", "Home", Path(temp), "127.0.0.1")
            task = asyncio.create_task(source._advertise())
            try:
                source._on_event("register", ["41234", "CPath=/", "VERSION=1.0", "Stack=SP"])
                await asyncio.sleep(0.01)
                name, _mac, address, port, service_type, properties = advertisers[0].args
                self.assertEqual((name, address, port, service_type), ("Home", "127.0.0.1", 41234, SPOTIFY_TYPE))
                self.assertEqual(properties, {"CPath": "/", "VERSION": "1.0", "Stack": "SP"})
                advertisers[0].start.assert_awaited_once()
                source._on_event("unregister", [])
                await asyncio.sleep(0.01)
                advertisers[0].close.assert_awaited_once()
                self.assertIsNone(source._advertiser)
            finally:
                task.cancel()
                await asyncio.gather(task, return_exceptions=True)

    async def test_pipe_backpressure_keeps_oldest_audio_instead_of_skipping_ahead(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            source = SpotifyInput("solo", "Solo", Path(temp), "127.0.0.1")
            source._discard = False
            source._reader = asyncio.StreamReader()
            first = b"\x01\x00\x02\x00" * (MAX_PCM_BYTES // 4)
            source._reader.feed_data(first + b"\x03\x00\x04\x00" * (CHUNK_BYTES // 4))
            task = asyncio.create_task(source._read_audio())
            try:
                await asyncio.sleep(0.01)
                self.assertEqual(len(source._pcm), MAX_PCM_BYTES)
                self.assertEqual(source.read_pcm(), first[:CHUNK_BYTES])
                await asyncio.sleep(0.01)
                self.assertEqual(len(source._pcm), MAX_PCM_BYTES)
                self.assertEqual(source._pcm[:4], first[CHUNK_BYTES:CHUNK_BYTES + 4])
            finally:
                task.cancel()
                await asyncio.gather(task, return_exceptions=True)

    async def test_dropping_pcm_on_events_keeps_frames_aligned(self) -> None:
        # Pipe reads split S16 stereo frames anywhere.
        frames = np.arange(4 * CHUNK_FRAMES, dtype="<i2")
        stream = np.column_stack((frames, -frames)).astype("<i2").tobytes()  # L = k, R = -k.
        for events, discarding in ((["playing"], True), (["seek"], False), (["stopped", "playing"], False)):
            with self.subTest(events=events), tempfile.TemporaryDirectory() as temp:
                source = SpotifyInput("solo", "Solo", Path(temp), "127.0.0.1")
                source._discard = discarding
                source._reader = asyncio.StreamReader()
                task = asyncio.create_task(source._read_audio())
                try:
                    source._reader.feed_data(stream[:1001])  # Not a whole number of frames.
                    await asyncio.sleep(0.01)
                    for event in events:
                        source._on_event(event, [])
                    source._reader.feed_data(stream[1001:])
                    await asyncio.sleep(0.01)
                    pcm = np.frombuffer(source.read_pcm(), dtype="<i2")
                    self.assertEqual(len(pcm), CHUNK_SAMPLES)
                    self.assertTrue(np.all(pcm[0::2] == -pcm[1::2]), "L/R pairs shifted")
                    self.assertTrue(np.all(np.diff(pcm[0::2]) == 1), "frames not contiguous")
                finally:
                    task.cancel()
                    await asyncio.gather(task, return_exceptions=True)

    async def test_start_runs_one_device_per_target_with_private_state_and_librespot_ids(self) -> None:
        lib = Mock()
        lib.gl_start.side_effect = [1, 2, -1]
        with tempfile.TemporaryDirectory() as temp, patch("sendspin_bridge.spotify._lib", return_value=lib):
            first = SpotifyInput("group:home", "Home", Path(temp), "127.0.0.1")
            second = SpotifyInput("stereo:pair", "Pair", Path(temp), "127.0.0.1")
            try:
                await first.start()
                await second.start()
                (name, device_id, state, pcm_fd, event_fd), other = (call.args for call in lib.gl_start.call_args_list)
                self.assertEqual(name, b"Home")
                # librespot derived its device ID this way: paired Spotify apps keep seeing the same device.
                self.assertEqual(device_id, hashlib.sha1(b"Home").hexdigest().encode())
                self.assertNotEqual(state, other[2])
                self.assertEqual(first._pipes, [pcm_fd, event_fd])
            finally:
                await first.close()
                await second.close()
            self.assertEqual([call.args for call in lib.gl_stop.call_args_list], [(1,), (2,)])
            self.assertEqual(first._pipes, [])
            failing = SpotifyInput("solo", "Solo", Path(temp), "127.0.0.1")
            with self.assertRaises(RuntimeError):
                await failing.start()
            self.assertEqual((failing._pipes, failing._event_fd, failing._transport), ([], None, None))

    @unittest.skipUnless(LIBRARY.exists(), "libgolibrespot.so is built by the Docker image")
    async def test_real_go_librespot_serves_pairing_on_the_advertised_port(self) -> None:
        advertisers: list[Mock] = []
        with tempfile.TemporaryDirectory() as temp, \
                patch("sendspin_bridge.spotify.Advertiser", side_effect=_advertiser_factory(advertisers)):
            source = SpotifyInput("solo", "Bridge Test", Path(temp), "127.0.0.1")
            await source.start()
            try:
                for _ in range(300):  # go-librespot resolves Spotify's access points before pairing.
                    if advertisers:
                        break
                    await asyncio.sleep(0.1)
                else:
                    self.skipTest("go-librespot did not register: no access to Spotify?")
                url = f"http://127.0.0.1:{advertisers[0].args[3]}/?action=getInfo"
                info = await asyncio.to_thread(lambda: json.load(urllib.request.urlopen(url, timeout=5)))
                self.assertEqual(info["remoteName"], "Bridge Test")
                self.assertEqual(info["deviceID"], hashlib.sha1(b"Bridge Test").hexdigest())
            finally:
                await source.close()
            advertisers[0].close.assert_awaited()


if __name__ == "__main__":
    unittest.main()
