FROM golang:1.24.1-bookworm

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
    apply_patch third_party/sendspin-go patches/sendspin-go.patch

CMD ["go", "test", "./..."]
