from __future__ import annotations

import unittest

import numpy as np

from sendspin_bridge.audio import CHUNK_SAMPLES, GroupBuffer, mixed_bytes


def pcm(value: int) -> bytes:
    return np.full(CHUNK_SAMPLES, value, dtype="<i2").tobytes()


def stereo_pcm(left: int, right: int) -> bytes:
    samples = np.empty(CHUNK_SAMPLES, dtype="<i2")
    samples[::2], samples[1::2] = left, right
    return samples.tobytes()


class GroupBufferTests(unittest.TestCase):
    def test_positive_delay_holds_back_group_audio(self) -> None:
        chunks = iter((pcm(100), pcm(200)))
        group = GroupBuffer(lambda _frames: next(chunks))
        first = np.zeros(CHUNK_SAMPLES, dtype=np.int32)
        group.mix_into(first, playback_chunk=0, delay_ms=0)
        held = np.zeros(CHUNK_SAMPLES, dtype=np.int32)
        group.mix_into(held, playback_chunk=1, delay_ms=20)
        current = np.zeros(CHUNK_SAMPLES, dtype=np.int32)
        group.mix_into(current, playback_chunk=1, delay_ms=0)
        self.assertTrue(np.all(first == 100))
        self.assertTrue(np.all(held == 100))
        self.assertTrue(np.all(current == 200))

    def test_negative_delay_advances_group_audio(self) -> None:
        chunks = iter((pcm(100), pcm(200), pcm(300)))
        group = GroupBuffer(lambda _frames: next(chunks))
        normal = np.zeros(CHUNK_SAMPLES, dtype=np.int32)
        group.mix_into(normal, playback_chunk=0, delay_ms=0)
        advanced = np.zeros(CHUNK_SAMPLES, dtype=np.int32)
        group.mix_into(advanced, playback_chunk=1, delay_ms=-20)
        self.assertTrue(np.all(normal == 100))
        self.assertTrue(np.all(advanced == 300))

    def test_stereo_routes_one_side_to_both_output_channels(self) -> None:
        group = GroupBuffer(lambda _frames: stereo_pcm(111, 222))
        left, right, normal = (np.zeros(CHUNK_SAMPLES, dtype=np.int32) for _ in range(3))
        group.mix_into(left, 0, 0, channel=0)
        group.mix_into(right, 0, 0, channel=1)
        group.mix_into(normal, 0, 0)
        self.assertTrue(np.all(left == 111))
        self.assertTrue(np.all(right == 222))
        self.assertTrue(np.all(normal[::2] == 111))
        self.assertTrue(np.all(normal[1::2] == 222))

    def test_offset_never_shifts_by_half_a_stereo_frame(self) -> None:
        group = GroupBuffer(lambda _frames: stereo_pcm(111, 222))
        output = np.zeros(CHUNK_SAMPLES, dtype=np.int32)
        group.mix_into(output, playback_chunk=1, delay_ms=5)
        self.assertEqual((output[0], output[1]), (111, 222))

    def test_mix_saturates_to_s16(self) -> None:
        group = GroupBuffer(lambda _frames: pcm(100))
        output = np.full(CHUNK_SAMPLES, 32_760, dtype=np.int32)
        group.mix_into(output, playback_chunk=0, delay_ms=0)
        result = np.frombuffer(mixed_bytes(output), dtype="<i2")
        self.assertTrue(np.all(result == 32_767))


if __name__ == "__main__":
    unittest.main()
