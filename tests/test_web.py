from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from airplay_sendspin.registry import Registry
from airplay_sendspin.web import registry_from_payload, registry_to_payload


class ConfigPayloadTests(unittest.TestCase):
    def test_payload_round_trip_saves_a_group(self) -> None:
        payload = {
            "airplay_suffix": " (Sendspin)",
            "speakers": [
                {
                    "id": "kitchen",
                    "airplay_name": "Kitchen",
                    "direction": "outbound",
                    "port": 0,
                    "client_id": "client-kitchen",
                    "hidden": True,
                    "delay_ms": -20,
                    "endpoint": {"instance": "", "host": "192.0.2.10", "port": 8928, "path": "/sendspin"},
                }
            ],
            "groups": [{"id": "home", "airplay_name": "Home", "port": 0, "speaker_ids": ["kitchen"]}],
        }
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.xml"
            registry = registry_from_payload(payload, str(path), 7000, 10)
            registry.save()
            restored = Registry.load(path)
            restored_payload = registry_to_payload(restored)
            self.assertEqual(restored_payload["airplay_suffix"], " (Sendspin)")
            self.assertEqual(restored_payload["speakers"][0]["delay_ms"], -20)
            self.assertEqual(restored_payload["groups"][0]["speaker_ids"], ["kitchen"])

    def test_payload_rejects_an_unknown_group_member(self) -> None:
        payload = {"airplay_suffix": "", "speakers": [], "groups": [{"id": "home", "speaker_ids": ["missing"]}]}
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(ValueError, "unknown"):
                registry_from_payload(payload, str(Path(directory) / "config.xml"), 7000, 10)


if __name__ == "__main__":
    unittest.main()
