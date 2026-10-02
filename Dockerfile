FROM node:22-bookworm-slim AS web-build

WORKDIR /web

COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend ./
RUN npm run build

# go-librespot runs inside the bridge: golibrespot/ builds it as a C shared library for CFFI.
# Patched so the bridge publishes its mDNS record with python-zeroconf.
FROM golang:1.25-bookworm AS spotify-build
RUN apt-get update && apt-get install -y --no-install-recommends \
    libasound2-dev libflac-dev libmpg123-dev libogg-dev libvorbis-dev patch \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY dependencies.lock ./
RUN set -eux; \
    . ./dependencies.lock; \
    git clone --quiet https://github.com/devgianlu/go-librespot.git third_party/go-librespot; \
    git -C third_party/go-librespot checkout --quiet --detach "$GO_LIBRESPOT_REF"
COPY patches/go-librespot ./patches/go-librespot
RUN for patch_file in patches/go-librespot/*.patch; do \
      patch -p1 -d third_party/go-librespot < "$patch_file"; \
    done
COPY golibrespot ./golibrespot
RUN cd golibrespot && go build -trimpath -buildmode=c-shared -o /out/libgolibrespot.so .

FROM python:3.13-bookworm AS build

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    git \
    ca-certificates \
    patch \
    pkg-config \
    libasound2 libflac12 libmpg123-0 libogg0 libvorbis0a libvorbisenc2 \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /src

COPY dependencies.lock ./
RUN set -eux; \
    . ./dependencies.lock; \
    git clone --quiet https://github.com/philippe44/libraop.git third_party/libraop; \
    git -C third_party/libraop checkout --detach "$LIBRAOP_REF"; \
    git -C third_party/libraop submodule update --init --recursive --jobs 8; \
    test "$(git -C third_party/libraop rev-parse HEAD)" = "$LIBRAOP_REF"

COPY patches/libraop ./patches/libraop
RUN set -eux; \
    for patch_file in patches/libraop/*.patch; do \
      [ -f "$patch_file" ] || continue; \
      patch -p1 -d third_party/libraop < "$patch_file"; \
    done; \
    make -C third_party/libraop STATIC=1 cleanlib; \
    make -C third_party/libraop STATIC=1 lib

COPY pyproject.toml setup.py build_raop.py ./
COPY src ./src
COPY --from=web-build /out ./src/sendspin_bridge/web
COPY --from=spotify-build /out/libgolibrespot.so ./src/sendspin_bridge/
COPY tests ./tests
# QEMU on ARM64 can report the host architecture; CFFI needs the requested target.
ARG TARGETARCH
RUN LIBRAOP_ROOT=/src/third_party/libraop python -m pip install --no-cache-dir --prefix=/install . \
 && PYTHONPATH=/src/src:/install/lib/python3.13/site-packages python -m unittest discover -s tests -v \
 && PYTHONPATH=/install/lib/python3.13/site-packages python -c 'from sendspin_bridge import _raop, spotify; assert _raop.lib.bridge_receiver_port == _raop.lib.bridge_receiver_port; assert spotify.LIBRARY.exists()'

FROM python:3.13-slim-bookworm AS runtime

# Pillow builds from source on arm/v7 and links against the build image's codecs.
RUN apt-get update && apt-get install -y --no-install-recommends \
    libatomic1 \
    liblcms2-2 \
    libopenjp2-7 \
    libstdc++6 \
    libtiff6 \
    libxcb1 \
    libasound2 libflac12 libmpg123-0 libogg0 libvorbis0a libvorbisenc2 \
 && rm -rf /var/lib/apt/lists/*

COPY --from=build /install /usr/local
RUN python -m sendspin_bridge -h >/dev/null

CMD ["python", "-m", "sendspin_bridge", "-config", "/data/config.xml"]

# Home Assistant add-on: config.xml lives in the user-editable addon_config.
FROM runtime AS addon
ARG BUILD_ARCH
ARG BUILD_VERSION
LABEL io.hass.type="addon" io.hass.arch="${BUILD_ARCH}" io.hass.version="${BUILD_VERSION}"
CMD ["python", "-m", "sendspin_bridge", "-config", "/config/config.xml"]
