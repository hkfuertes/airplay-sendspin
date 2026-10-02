from __future__ import annotations

import asyncio
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import AsyncMock, Mock, patch

from sendspin_bridge.audio import CHUNK_BYTES
from sendspin_bridge.spotify import MAX_PCM_BYTES, SPOTIFY_TYPE, SpotifyInput


class SpotifyTests(unittest.IsolatedAsyncioTestCase):
    async def test_event_helper_delivers_play_pause_and_seek_without_touching_pcm_stdout(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            fifo = Path(temp) / "events"
            os.mkfifo(fifo)
            source = SpotifyInput("librespot", "solo", "Solo", Path(temp), "127.0.0.1")
            source._fifo = os.open(fifo, os.O_RDWR | os.O_NONBLOCK)
            source.on_reset = Mock()
            script = Path(__file__).parents[1] / "src/sendspin_bridge/spotify_event.sh"
            try:
                for event, playing in (("playing", True), ("seeked", True), ("paused", False), ("stopped", False)):
                    env = {**os.environ, "PLAYER_EVENT": event, "SENDSPIN_EVENT_FIFO": str(fifo)}
                    process = await asyncio.create_subprocess_exec("sh", str(script), env=env, stdout=asyncio.subprocess.PIPE)
                    stdout, _ = await process.communicate()
                    self.assertEqual((process.returncode, stdout), (0, b""))
                    source._pcm.extend(b"\x01" * CHUNK_BYTES)
                    source._read_events()
                    self.assertEqual(source.playing, playing)
                    self.assertEqual(source.read_pcm(), b"")
                self.assertEqual(source.on_reset.call_count, 4)
            finally:
                os.close(source._fifo)
                source._fifo = None

    async def test_pipe_backpressure_keeps_oldest_audio_instead_of_skipping_ahead(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            source = SpotifyInput("librespot", "solo", "Solo", Path(temp), "127.0.0.1")
            source.playing = True
            reader = asyncio.StreamReader()
            source.process = Mock(stdout=reader)
            first = b"\x01\x00\x02\x00" * (MAX_PCM_BYTES // 4)
            reader.feed_data(first + b"\x03\x00\x04\x00" * (CHUNK_BYTES // 4))
            task = asyncio.create_task(source._read_audio())
            try:
                await asyncio.sleep(0.01)
                self.assertEqual(len(source._pcm), MAX_PCM_BYTES)
                self.assertEqual(source.read_pcm(), first[:CHUNK_BYTES])
                await asyncio.sleep(0.01)
                self.assertEqual(len(source._pcm), MAX_PCM_BYTES)
                self.assertEqual(source._pcm[:4], first[CHUNK_BYTES:CHUNK_BYTES + 4])
            finally:
                source._closing = True
                task.cancel()
                await asyncio.gather(task, return_exceptions=True)

    async def test_launch_uses_distinct_private_caches_and_bridge_mdns(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            proc = Mock(returncode=0, stdout=Mock(read=AsyncMock(return_value=b"")), stderr=Mock(readline=AsyncMock(return_value=b"")))
            advertisers: list[Mock] = []

            def advertise(*args):
                advertisers.append(Mock(args=args, start=AsyncMock(), close=AsyncMock()))
                return advertisers[-1]

            with patch("sendspin_bridge.spotify.asyncio.create_subprocess_exec", new_callable=AsyncMock, return_value=proc) as spawn, \
                    patch("sendspin_bridge.spotify.Advertiser", side_effect=advertise):
                first = SpotifyInput("librespot", "group:home", "Home", Path(temp), "127.0.0.1")
                second = SpotifyInput("librespot", "stereo:pair", "Pair", Path(temp), "127.0.0.1")
                try:
                    await first.start()
                    await second.start()
                    a, b = spawn.call_args_list
                    self.assertNotEqual(a.args[a.args.index("--system-cache") + 1], b.args[b.args.index("--system-cache") + 1])
                    self.assertIn("--backend", a.args)
                    self.assertEqual(a.args[a.args.index("--format") + 1], "S16")
                    self.assertEqual(a.kwargs["stdout"], asyncio.subprocess.PIPE)
                    self.assertNotEqual(a.kwargs["env"]["SENDSPIN_EVENT_FIFO"], b.kwargs["env"]["SENDSPIN_EVENT_FIFO"])
                    # librespot only serves pairing; the bridge publishes its mDNS record.
                    self.assertEqual(a.args[a.args.index("--zeroconf-backend") + 1], "external")
                    name, _mac, address, port, service_type, _properties = advertisers[0].args
                    self.assertEqual((name, address, service_type), ("Home", "127.0.0.1", SPOTIFY_TYPE))
                    self.assertEqual(str(port), a.args[a.args.index("--zeroconf-port") + 1])
                    advertisers[0].start.assert_awaited_once()
                finally:
                    await first.close()
                    await second.close()
            for advertiser in advertisers:
                advertiser.close.assert_awaited()


if __name__ == "__main__":
    unittest.main()
