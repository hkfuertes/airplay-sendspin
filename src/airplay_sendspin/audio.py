"""Fixed-grid PCM helpers for individual and grouped AirPlay inputs."""

from __future__ import annotations

from collections.abc import Callable

import numpy as np

SAMPLE_RATE = 44_100
CHANNELS = 2
CHUNK_MS = 20
CHUNK_FRAMES = SAMPLE_RATE * CHUNK_MS // 1000
CHUNK_SAMPLES = CHUNK_FRAMES * CHANNELS
CHUNK_BYTES = CHUNK_SAMPLES * 2
# ponytail: 640 ms covers the signed 500 ms offset; grow it only if larger offsets are supported.
CACHE_CHUNKS = 32


def chunk(pcm: bytes) -> np.ndarray:
    """Return exactly one S16_LE stereo chunk, padding an underrun with silence."""
    pcm = pcm[:CHUNK_BYTES]
    if len(pcm) < CHUNK_BYTES:
        pcm += b"\0" * (CHUNK_BYTES - len(pcm))
    return np.frombuffer(pcm, dtype="<i2")


def mixed_bytes(samples: np.ndarray) -> bytes:
    return np.clip(samples, -32768, 32767).astype("<i2").tobytes()


class GroupBuffer:
    """Caches one group input by playback chunk so every member sees the same PCM."""

    def __init__(self, read_pcm: Callable[[int], bytes]) -> None:
        self._read_pcm = read_pcm
        self._chunks = [np.zeros(CHUNK_SAMPLES, dtype=np.int16) for _ in range(CACHE_CHUNKS)]
        self._indices = [-1] * CACHE_CHUNKS
        self._next: int | None = None

    def reset(self) -> None:
        self._indices = [-1] * CACHE_CHUNKS
        self._next = None

    def mix_into(self, output: np.ndarray, playback_chunk: int, delay_ms: int) -> None:
        """Mix this group at a signed millisecond offset into a 20 ms S32 buffer."""
        offset = delay_ms * SAMPLE_RATE * CHANNELS // 1000
        start = playback_chunk * CHUNK_SAMPLES - offset
        if start < 0:
            return
        end = start + CHUNK_SAMPLES
        for index in range(start // CHUNK_SAMPLES, (end - 1) // CHUNK_SAMPLES + 1):
            source = self._chunk_at(index)
            if source is None:
                continue
            left = max(start, index * CHUNK_SAMPLES)
            right = min(end, (index + 1) * CHUNK_SAMPLES)
            output[left - start : right - start] += source[left - index * CHUNK_SAMPLES : right - index * CHUNK_SAMPLES]

    def _chunk_at(self, index: int) -> np.ndarray | None:
        if self._next is None or index - self._next >= CACHE_CHUNKS:
            self._next = index
        while self._next <= index:
            slot = self._next % CACHE_CHUNKS
            self._chunks[slot] = chunk(self._read_pcm(CHUNK_FRAMES)).copy()
            self._indices[slot] = self._next
            self._next += 1
        slot = index % CACHE_CHUNKS
        return self._chunks[slot] if self._indices[slot] == index else None
