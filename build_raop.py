"""Build the small CFFI boundary around the patched libraop receiver."""

from __future__ import annotations

import os
import platform
from pathlib import Path

from cffi import FFI

ffibuilder = FFI()
ffibuilder.cdef("""
typedef struct bridge_receiver bridge_receiver_t;
bridge_receiver_t *bridge_receiver_new(const char *name, const uint8_t mac[6],
    const uint8_t ipv4[4], uint16_t port_base, uint16_t port_range);
void bridge_receiver_delete(bridge_receiver_t *receiver);
uint16_t bridge_receiver_port(const bridge_receiver_t *receiver);
size_t bridge_receiver_read_pcm(bridge_receiver_t *receiver, int16_t *dst, size_t capacity_frames);
int bridge_receiver_read_event(bridge_receiver_t *receiver, double *volume);
""")

root = Path(os.environ.get("LIBRAOP_ROOT", "third_party/libraop")).resolve()
arch = {"amd64": "x86_64", "x86_64": "x86_64", "arm64": "aarch64", "aarch64": "aarch64", "arm": "arm", "armv7l": "arm", "armv8l": "arm"}.get(
    os.environ.get("TARGETARCH", platform.machine()), platform.machine()
)

ffibuilder.set_source(
    "sendspin_bridge._raop",
    '#include "bridge.h"',
    sources=["src/sendspin_bridge/native/bridge.c"],
    include_dirs=[str(path) for path in [
        "src/sendspin_bridge/native",
        root / "src",
        root / "src/inc",
        root / "crosstools/src",
        root / "dmap-parser",
        root / "libmdns/targets/include/mdnssvc",
        root / "libmdns/targets/include/mdnssd",
        root / f"libopenssl/targets/linux/{arch}/include",
        root / "libcodecs/targets/include/addons",
        root / "libcodecs/targets/include/flac",
        root / "libcodecs/targets/include/shine",
        root / "libcodecs/targets/include/faac",
    ]],
    library_dirs=[str(path) for path in [
        root / f"lib/linux/{arch}",
        root / f"libcodecs/targets/linux/{arch}",
        root / f"libmdns/targets/linux/{arch}",
        root / f"libopenssl/targets/linux/{arch}",
    ]],
    libraries=["raop", "codecs", "mdns", "openssl", "stdc++", "pthread", "dl", "m", "atomic"],
    extra_compile_args=["-fPIC"],
)

if __name__ == "__main__":
    ffibuilder.compile(verbose=True)
