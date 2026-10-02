"""Spotify Connect PCM input: go-librespot runs in-process through CFFI (libgolibrespot.so)."""

from __future__ import annotations

import asyncio
import functools
import hashlib
import logging
import os
import time
from collections.abc import Callable
from pathlib import Path
from typing import Any

from cffi import FFI

from .airplay import Advertiser, virtual_mac
from .audio import CHUNK_BYTES, CHUNK_FRAMES

LOG = logging.getLogger(__name__)
LIBRARY = Path(__file__).with_name("libgolibrespot.so")
# go-librespot only serves pairing (patches/go-librespot); the bridge publishes this record itself.
SPOTIFY_TYPE = "_spotify-connect._tcp.local."
# ponytail: 500 ms of PCM; block the pipe reader at this limit so go-librespot cannot decode ahead.
MAX_PCM_BYTES = CHUNK_BYTES * 25

_ffi = FFI()
_ffi.cdef("int gl_start(char *name, char *device_id, char *state_path, int pcm_fd, int event_fd); void gl_stop(int id);")


@functools.cache
def _lib() -> Any:
    return _ffi.dlopen(str(LIBRARY))


class SpotifyInput:
    """One Spotify Connect device; go-librespot writes raw 44.1 kHz S16 stereo PCM to a pipe."""

    def __init__(
        self, key: str, name: str, state_dir: Path, address: str, on_reset: Callable[[], None] | None = None,
    ) -> None:
        self.key = key
        self.name = name
        self.state_dir = state_dir
        self.address = address
        self.on_reset = on_reset
        self.playing = False
        self.started_at = 0
        self._handle: int | None = None
        self._discard = True  # Drop PCM until playback starts and after it stops; a pause keeps it.
        self._pcm = bytearray()
        self._space = asyncio.Event()
        self._space.set()
        self._reader: asyncio.StreamReader | None = None
        self._transport: asyncio.ReadTransport | None = None
        self._event_fd: int | None = None
        self._events = bytearray()
        self._pipes: list[int] = []
        self._registrations: asyncio.Queue[tuple[int, dict[str, str]] | None] = asyncio.Queue()
        self._advertiser: Advertiser | None = None
        self._tasks: list[asyncio.Task[None]] = []

    async def start(self) -> None:
        digest = hashlib.sha256(self.key.encode()).hexdigest()[:16]
        cache = self.state_dir / "spotify" / digest
        cache.mkdir(mode=0o700, parents=True, exist_ok=True)
        cache.chmod(0o700)  # go-librespot stores reusable Spotify credentials here.
        loop = asyncio.get_running_loop()
        pcm_fd, pcm_w = os.pipe()
        self._event_fd, event_w = os.pipe()
        self._pipes = [pcm_w, event_w]  # go-librespot opens them as /dev/fd/N; keep them until gl_stop.
        try:
            os.set_blocking(self._event_fd, False)
            loop.add_reader(self._event_fd, self._read_events)
            self._reader = asyncio.StreamReader()
            reader = self._reader
            self._transport, _ = await loop.connect_read_pipe(
                lambda: asyncio.StreamReaderProtocol(reader), os.fdopen(pcm_fd, "rb", buffering=0),
            )
            # librespot derived the Connect device ID from the name; keep it so paired apps see the same device.
            device_id = hashlib.sha1(self.name.encode()).hexdigest()
            state = str(cache / "go-librespot.json").encode()
            handle = _lib().gl_start(self.name.encode(), device_id.encode(), state, pcm_w, event_w)
            if handle < 0:
                raise RuntimeError(f"go-librespot failed to start for {self.key}")
        except BaseException:
            await self.close()
            raise
        self._handle = handle
        self._tasks = [asyncio.create_task(self._read_audio()), asyncio.create_task(self._advertise())]
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
        if self._event_fd is None:
            return
        try:
            self._events.extend(os.read(self._event_fd, 4096))
        except BlockingIOError:
            return
        while b"\n" in self._events:
            raw, _, remainder = self._events.partition(b"\n")
            self._events = bytearray(remainder)
            event, *args = raw.decode("utf-8", errors="replace").split() or [""]
            self._on_event(event, args)

    def _on_event(self, event: str, args: list[str]) -> None:
        if event == "register":
            properties = dict(item.split("=", 1) for item in args[1:] if "=" in item)
            self._registrations.put_nowait((int(args[0]), properties))
        elif event == "unregister":
            self._registrations.put_nowait(None)
        elif event == "playing":
            self._discard = False
            if not self.playing:  # A start or resume; go-librespot repeats it on every track change.
                self.playing = True
                self.started_at = time.monotonic_ns()
        elif event == "paused":
            self.playing = False  # Keep buffered audio: resuming continues exactly where it paused.
        elif event in ("seek", "stopped", "inactive"):
            if event != "seek":
                self.playing = False
                self._discard = True
            self._drop_whole_frames()
            self._space.set()
            if self.on_reset is not None:
                self.on_reset()
        # not_playing/will_play mark gapless track changes: the next track's audio is already flowing.

    async def _read_audio(self) -> None:
        assert self._reader is not None
        while True:
            if len(self._pcm) >= MAX_PCM_BYTES:
                self._space.clear()
                await self._space.wait()
                continue
            data = await self._reader.read(min(CHUNK_BYTES * 4, MAX_PCM_BYTES - len(self._pcm)))
            if not data:
                return
            self._pcm.extend(data)
            if self._discard:
                self._drop_whole_frames()

    def _drop_whole_frames(self) -> None:
        # Pipe reads split S16 stereo frames anywhere; dropping a partial frame would shift every
        # later sample (heard as noise). Keep the partial tail.
        del self._pcm[: len(self._pcm) // 4 * 4]

    async def _advertise(self) -> None:
        while True:
            registration = await self._registrations.get()
            try:
                if self._advertiser is not None:
                    await self._advertiser.close()
                    self._advertiser = None
                if registration is not None:
                    port, properties = registration
                    self._advertiser = Advertiser(
                        self.name, virtual_mac(f"spotify:{self.key}"), self.address, port, SPOTIFY_TYPE, properties,
                    )
                    await self._advertiser.start()
            except Exception:
                LOG.exception("Spotify Connect advertisement for %s failed", self.key)

    async def close(self) -> None:
        self.playing = False
        self._discard = True
        if self._handle is not None:
            await asyncio.to_thread(_lib().gl_stop, self._handle)
            self._handle = None
        for task in self._tasks:
            task.cancel()
        await asyncio.gather(*self._tasks, return_exceptions=True)
        self._tasks.clear()
        if self._advertiser is not None:
            await self._advertiser.close()
            self._advertiser = None
        if self._event_fd is not None:
            asyncio.get_running_loop().remove_reader(self._event_fd)
            os.close(self._event_fd)
            self._event_fd = None
        if self._transport is not None:
            self._transport.close()
            self._transport = None
        for fd in self._pipes:
            os.close(fd)
        self._pipes.clear()
        self._pcm.clear()
        self._space.set()
