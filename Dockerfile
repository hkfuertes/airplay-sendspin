FROM golang:1.24.1-bookworm AS build

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    libasound2-dev \
    libopus-dev \
    libopusfile-dev \
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
    test "$(git -C third_party/libraop rev-parse HEAD)" = "$LIBRAOP_REF"; \
    git clone --quiet https://github.com/Sendspin/sendspin-go.git third_party/sendspin-go; \
    git -C third_party/sendspin-go checkout --detach "$SENDSPIN_GO_REF"; \
    test "$(git -C third_party/sendspin-go rev-parse HEAD)" = "$SENDSPIN_GO_REF"

COPY go.mod ./
RUN go mod download

COPY patches ./patches
RUN set -eux; \
    apply_dir() { \
      target="$1"; patch_dir="$2"; applied=0; \
      for patch_file in "$patch_dir"/*.patch; do \
        [ -f "$patch_file" ] || continue; \
        patch -p1 -d "$target" < "$patch_file"; \
        applied=1; \
      done; \
      [ "$applied" -eq 1 ]; \
    }; \
    apply_dir third_party/libraop patches/libraop; \
    apply_dir third_party/sendspin-go patches/sendspin

COPY . .
RUN make -C third_party/libraop STATIC=1 cleanlib; \
    make -C third_party/libraop STATIC=1 lib; \
    go build -buildvcs=false -o /usr/local/bin/airplay-sendspin ./cmd/goplay2-sendspin

RUN go vet ./cmd/goplay2-sendspin ./internal/... \
 && go test -count=1 -buildvcs=false ./cmd/goplay2-sendspin ./internal/... \
 && cd third_party/sendspin-go \
 && go test -count=1 ./pkg/sendspin ./pkg/discovery

FROM debian:bookworm-slim AS runtime

RUN apt-get update && apt-get install -y --no-install-recommends \
    libasound2 \
    libopus0 \
    libopusfile0 \
    libstdc++6 \
 && rm -rf /var/lib/apt/lists/*

COPY --from=build /usr/local/bin/airplay-sendspin /usr/local/bin/airplay-sendspin
RUN /usr/local/bin/airplay-sendspin -h >/dev/null 2>&1

CMD ["/usr/local/bin/airplay-sendspin", "-config", "/data/config.xml"]

# Home Assistant add-on: config.xml lives in the user-editable addon_config.
FROM runtime AS addon
ARG BUILD_VERSION
LABEL io.hass.type="addon" io.hass.arch="amd64" io.hass.version="${BUILD_VERSION}"
CMD ["/usr/local/bin/airplay-sendspin", "-config", "/config/config.xml"]
