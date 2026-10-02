"""The small mDNS half of an AirPlay 1 receiver."""

from __future__ import annotations

import hashlib
import ipaddress
import socket
from dataclasses import dataclass

from zeroconf import IPVersion, ServiceInfo
from zeroconf.asyncio import AsyncZeroconf

RAOP_TYPE = "_raop._tcp.local."
RAOP_PROPERTIES = {
    "tp": "UDP",
    "sm": "false",
    "sv": "false",
    "ek": "1",
    "et": "0,1",
    "md": "0,1,2",
    "cn": "0,1",
    "ch": "2",
    "ss": "16",
    "sr": "44100",
    "vn": "3",
    "txtvers": "1",
    "am": "AirPort10,115",
}


def lan_ipv4() -> str:
    """Return the IPv4 address selected by the default route."""
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
        sock.connect(("192.0.2.1", 80))
        return str(sock.getsockname()[0])


def virtual_mac(key: str) -> bytes:
    mac = bytearray(hashlib.sha256(key.encode()).digest()[:6])
    mac[0] = mac[0] & 0xFE | 0x02
    return bytes(mac)


def service_name(name: str, mac: bytes) -> str:
    return (f"{mac.hex().upper()}@{name}")[:63]


def host_name(mac: bytes) -> str:
    return f"raop-{mac.hex()}.local."


@dataclass
class Advertiser:
    """A registered mDNS service: `_raop._tcp` by default, or e.g. `_spotify-connect._tcp`."""

    name: str
    mac: bytes
    address: str
    port: int
    service_type: str = RAOP_TYPE
    properties: dict[str, str] | None = None
    _zeroconf: AsyncZeroconf | None = None
    _info: ServiceInfo | None = None

    def __post_init__(self) -> None:
        if not self.name or len(self.mac) != 6 or not self.port:
            raise ValueError("AirPlay name, MAC, and port are required")
        address = ipaddress.IPv4Address(self.address)
        raop = self.service_type == RAOP_TYPE
        instance = service_name(self.name, self.mac) if raop else self.name
        self._info = ServiceInfo(
            type_=self.service_type,
            name=f"{instance}.{self.service_type}",
            server=host_name(self.mac),
            addresses=[address.packed],
            port=self.port,
            properties=RAOP_PROPERTIES if raop else self.properties or {},
        )
    async def start(self) -> None:
        if self._zeroconf is not None:
            return
        zeroconf = AsyncZeroconf(ip_version=IPVersion.V4Only, interfaces=[self.address])
        try:
            await zeroconf.async_register_service(self._info)
        except BaseException:
            await zeroconf.async_close()
            raise
        self._zeroconf = zeroconf

    async def close(self) -> None:
        zeroconf, info = self._zeroconf, self._info
        self._zeroconf = None
        self._info = None
        if zeroconf is None:
            return
        try:
            if info is not None:
                await zeroconf.async_unregister_service(info)
        finally:
            await zeroconf.async_close()
