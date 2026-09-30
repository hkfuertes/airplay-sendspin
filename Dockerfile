FROM node:22-bookworm-slim AS web-build

WORKDIR /web

COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend ./
RUN npm run build

FROM python:3.13-bookworm AS build

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    git \
    ca-certificates \
    patch \
    pkg-config \
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
COPY --from=web-build /out ./src/airplay_sendspin/web
COPY tests ./tests
RUN LIBRAOP_ROOT=/src/third_party/libraop python -m pip install --no-cache-dir --prefix=/install . \
 && PYTHONPATH=/src/src:/install/lib/python3.13/site-packages python -m unittest discover -s tests -v \
 && PYTHONPATH=/install/lib/python3.13/site-packages python -c 'from airplay_sendspin import _raop; assert _raop.lib.bridge_receiver_port == _raop.lib.bridge_receiver_port'

FROM python:3.13-slim-bookworm AS runtime

RUN apt-get update && apt-get install -y --no-install-recommends \
    libatomic1 \
    libstdc++6 \
 && rm -rf /var/lib/apt/lists/*

COPY --from=build /install /usr/local
RUN python -m airplay_sendspin -h >/dev/null

CMD ["python", "-m", "airplay_sendspin", "-config", "/data/config.xml"]

# Home Assistant add-on: config.xml lives in the user-editable addon_config.
FROM runtime AS addon
ARG BUILD_ARCH
ARG BUILD_VERSION
LABEL io.hass.type="addon" io.hass.arch="${BUILD_ARCH}" io.hass.version="${BUILD_VERSION}"
CMD ["python", "-m", "airplay_sendspin", "-config", "/config/config.xml"]
