FROM golang:1.24.1-bookworm AS build

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    libasound2-dev \
    libopus-dev \
    libopusfile-dev \
    patch \
    pkg-config \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /src

COPY go.mod ./
COPY third_party/sendspin-go/go.mod third_party/sendspin-go/go.sum ./third_party/sendspin-go/
RUN go mod download

COPY . .

# A checkout records pinned upstream submodules; these patches are the bridge
# fork. Reverse-check first so local development trees with applied changes
# build too.
RUN set -eux; \
    apply_patch() { \
      if patch -R -p1 --dry-run -d "$1" < "$2" >/dev/null 2>&1; then return; fi; \
      patch -p1 -d "$1" < "$2"; \
    }; \
    apply_patch third_party/libraop patches/libraop-pcm.patch; \
    apply_patch third_party/sendspin-go patches/sendspin-go.patch; \
    make -C third_party/libraop STATIC=1 cleanlib; \
    make -C third_party/libraop STATIC=1 lib; \
    go build -buildvcs=false -o /usr/local/bin/airplay-sendspin ./cmd/goplay2-sendspin

RUN go vet ./cmd/goplay2-sendspin ./internal/... \
 && go test -count=1 -buildvcs=false ./cmd/goplay2-sendspin ./internal/... \
 && cd third_party/sendspin-go \
 && go test -count=1 ./pkg/sendspin ./pkg/discovery

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    libasound2 \
    libopus0 \
    libopusfile0 \
    libstdc++6 \
 && rm -rf /var/lib/apt/lists/*

COPY --from=build /usr/local/bin/airplay-sendspin /usr/local/bin/airplay-sendspin
RUN /usr/local/bin/airplay-sendspin -h >/dev/null 2>&1

CMD ["/usr/local/bin/airplay-sendspin", "-config", "/data/config.xml"]
