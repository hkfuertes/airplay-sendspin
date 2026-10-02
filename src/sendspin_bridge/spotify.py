"""Spotify Connect PCM input using librespot's pipe backend (no Spotify SDK)."""

from __future__ import annotations

import asyncio
import hashlib
import logging
import os
import socket
import tempfile
import time
from pathlib import Path
from collections.abc import Callable

from .airplay import Advertiser, virtual_mac
from .audio import CHUNK_BYTES, CHUNK_FRAMES

LOG = logging.getLogger(__name__)
# librespot is patched with an "external" zeroconf backend; the bridge publishes this record itself.
SPOTIFY_TYPE = "_spotify-connect._tcp.local."
SPOTIFY_PROPERTIES = {"VERSION": "1.0", "CPath": "/"}
# ponytail: 500 ms of PCM; block the pipe reader at this limit so librespot cannot decode ahead.
MAX_PCM_BYTES = CHUNK_BYTES * 25


class SpotifyInput:
    """One Spotify Connect destination; stdout is raw 44.1 kHz S16 stereo PCM."""

    def __init__(
        self, binary: str, key: str, name: str, state_dir: Path, address: str,
        on_reset: Callable[[], None] | None = None,
    ) -> None:
        self.binary = binary
        self.key = key
        self.name = name
        self.state_dir = state_dir
        self.address = address
        self.on_reset = on_reset
        self.playing = False
        self.started_at = 0
        self.process: asyncio.subprocess.Process | None = None
        self._advertiser: Advertiser | None = None
        self._pcm = bytearray()
        self._space = asyncio.Event()
        self._space.set()
        self._events = bytearray()
        self._temp: tempfile.TemporaryDirectory[str] | None = None
        self._fifo: int | None = None
        self._tasks: list[asyncio.Task[None]] = []
        self._closing = False

    async def start(self) -> None:
        digest = hashlib.sha256(self.key.encode()).hexdigest()[:16]
        cache = self.state_dir / "spotify" / digest
        cache.mkdir(mode=0o700, parents=True, exist_ok=True)
        cache.chmod(0o700)  # librespot stores reusable Spotify credentials here.
        self._temp = tempfile.TemporaryDirectory(prefix="sendspin-spotify-")
        fifo_path = str(Path(self._temp.name) / "events")
        os.mkfifo(fifo_path, 0o600)
        self._fifo = os.open(fifo_path, os.O_RDWR | os.O_NONBLOCK)
        asyncio.get_running_loop().add_reader(self._fifo, self._read_events)
        env = os.environ.copy()
        env["SENDSPIN_EVENT_FIFO"] = fifo_path
        port = _free_port()
        try:
            self.process = await asyncio.create_subprocess_exec(
                self.binary, "--name", self.name, "--backend", "pipe",
                "--format", "S16", "--initial-volume", "100", "--system-cache", str(cache),
                "--zeroconf-backend", "external", "--zeroconf-port", str(port),
                "--onevent", str(Path(__file__).with_name("spotify_event.sh")), "--quiet",
                stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE, env=env,
            )
            self._advertiser = Advertiser(
                self.name, virtual_mac(f"spotify:{self.key}"), self.address, port, SPOTIFY_TYPE, SPOTIFY_PROPERTIES,
            )
            await self._advertiser.start()
        except BaseException:
            await self.close()
            raise
        self._tasks = [asyncio.create_task(self._read_audio()), asyncio.create_task(self._read_logs())]
        LOG.info("Spotify Connect target %r started (%s)", self.name, self.key)

    def read_pcm(self, frames: int = CHUNK_FRAMES) -> bytes:
        size = frames * 4
        if len(self._pcm) < size:
            return b""
        data = bytes(self._pcm[:size])
        del self._pcm[:size]
        self._space.set()
        return data

    def _read_events(self) -> None:
        if self._fifo is None:
            return
        try:
            self._events.extend(os.read(self._fifo, 4096))
        except BlockingIOError:
            return
        while b"\n" in self._events:
            raw, _, remainder = self._events.partition(b"\n")
            self._events = bytearray(remainder)
            event = raw.decode("ascii", errors="replace")
            if event in ("paused", "stopped", "seeked", "playing"):
                self._pcm.clear()
                self._space.set()
                if self.on_reset is not None:
                    self.on_reset()
                if event != "seeked":
                    self.playing = event == "playing"
                    if self.playing:
                        self.started_at = time.monotonic_ns()

    async def _read_audio(self) -> None:
        assert self.process is not None and self.process.stdout is not None
        try:
            while True:
                if len(self._pcm) >= MAX_PCM_BYTES:
                    self._space.clear()
                    await self._space.wait()
                    continue
                data = await self.process.stdout.read(min(CHUNK_BYTES * 4, MAX_PCM_BYTES - len(self._pcm)))
                if not data:
                    break
                if self.playing:
                    self._pcm.extend(data)
        finally:
            self.playing = False
            self._pcm.clear()
            if not self._closing:
                LOG.warning("Spotify Connect target %s exited", self.key)
                if self._advertiser is not None:
                    await self._advertiser.close()  # Don't advertise a dead pairing endpoint.

    async def _read_logs(self) -> None:
        assert self.process is not None and self.process.stderr is not None
        while line := await self.process.stderr.readline():
            LOG.info("librespot %s: %s", self.key, line.decode(errors="replace").rstrip())

    async def close(self) -> None:
        self._closing = True
        self.playing = False
        self._pcm.clear()
        self._space.set()
        if self._advertiser is not None:
            await self._advertiser.close()
            self._advertiser = None
        if self.process is not None:
            if self.process.returncode is None:
                self.process.terminate()
                try:
                    await asyncio.wait_for(self.process.wait(), timeout=3)
                except asyncio.TimeoutError:
                    self.process.kill()
                    await self.process.wait()
            self.process = None
        for task in self._tasks:
            task.cancel()
        await asyncio.gather(*self._tasks, return_exceptions=True)
        self._tasks.clear()
        if self._fifo is not None:
            asyncio.get_running_loop().remove_reader(self._fifo)
            os.close(self._fifo)
            self._fifo = None
        if self._temp is not None:
            self._temp.cleanup()
            self._temp = None


def _free_port() -> int:
    # ponytail: librespot binds after this probe closes; a port taken in between only fails this target.
    with socket.socket() as probe:
        probe.bind(("", 0))
        return probe.getsockname()[1]
