"""Python ownership of the patched libraop receiver."""

from __future__ import annotations

import ipaddress
from dataclasses import dataclass

PLAY = 1
FLUSH = 2
STOP = 4
VOLUME = 8


@dataclass
class Receiver:
    """A nonblocking pull interface over libraop's RAOP PCM callback."""

    name: str
    mac: bytes
    address: str
    port_base: int
    port_range: int

    def __post_init__(self) -> None:
        if not self.name or len(self.mac) != 6:
            raise ValueError("AirPlay name and six-byte MAC are required")
        if self.port_base and self.port_range < 3:
            raise ValueError("AirPlay port range must contain at least three ports")
        try:
            from ._raop import ffi, lib
        except ImportError as error:  # Makes pure-Python tests usable without libc.
            raise RuntimeError("the libraop CFFI extension is not installed") from error
        self._ffi = ffi
        self._lib = lib
        host = ipaddress.IPv4Address(self.address).packed
        self._receiver = lib.bridge_receiver_new(
            self.name.encode(), ffi.new("uint8_t[6]", self.mac), ffi.new("uint8_t[4]", host),
            self.port_base, self.port_range or 1,
        )
        if self._receiver == ffi.NULL:
            raise RuntimeError("create AirPlay receiver")
        self.port = int(lib.bridge_receiver_port(self._receiver))
        if not self.port:
            self.close()
            raise RuntimeError("AirPlay receiver did not bind an RTSP port")

    def read_pcm(self, frames: int) -> bytes:
        if frames < 1 or self._receiver == self._ffi.NULL:
            return b""
        samples = self._ffi.new("int16_t[]", frames * 2)
        count = int(self._lib.bridge_receiver_read_pcm(self._receiver, samples, frames))
        return bytes(self._ffi.buffer(samples, count * 4))

    def read_event(self) -> tuple[int, float] | None:
        if self._receiver == self._ffi.NULL:
            return None
        volume = self._ffi.new("double *")
        event = int(self._lib.bridge_receiver_read_event(self._receiver, volume))
        return (event, float(volume[0])) if event else None

    def close(self) -> None:
        receiver = getattr(self, "_receiver", None)
        if receiver is not None and receiver != self._ffi.NULL:
            self._lib.bridge_receiver_delete(receiver)
            self._receiver = self._ffi.NULL
