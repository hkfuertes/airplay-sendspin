from __future__ import annotations

import asyncio
import unittest
from unittest.mock import AsyncMock, patch

from sendspin_bridge.airplay import Advertiser


class AdvertiserTests(unittest.TestCase):
    def test_registers_without_blocking_the_event_loop(self) -> None:
        with patch("sendspin_bridge.airplay.AsyncZeroconf") as factory:
            zeroconf = factory.return_value
            zeroconf.async_register_service = AsyncMock()
            zeroconf.async_unregister_service = AsyncMock()
            zeroconf.async_close = AsyncMock()
            advertiser = Advertiser("Kitchen", b"\x02\x00\x00\x00\x00\x01", "192.0.2.1", 7000)
            info = advertiser._info
            factory.assert_not_called()

            asyncio.run(advertiser.start())
            zeroconf.async_register_service.assert_awaited_once_with(info)

            asyncio.run(advertiser.close())
            zeroconf.async_unregister_service.assert_awaited_once_with(info)
            zeroconf.async_close.assert_awaited_once()

    def test_other_service_types_use_plain_instance_and_own_txt(self) -> None:
        advertiser = Advertiser("Echo", b"\x02\x00\x00\x00\x00\x02", "192.0.2.1", 4070,
                                "_spotify-connect._tcp.local.", {"VERSION": "1.0", "CPath": "/"})
        info = advertiser._info
        assert info is not None
        self.assertEqual(info.name, "Echo._spotify-connect._tcp.local.")
        self.assertEqual(info.port, 4070)
        self.assertEqual(info.properties, {b"VERSION": b"1.0", b"CPath": b"/"})


if __name__ == "__main__":
    unittest.main()
