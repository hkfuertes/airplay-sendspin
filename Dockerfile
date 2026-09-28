FROM golang:1.24.1-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    libasound2-dev \
    libopus-dev \
    libopusfile-dev \
    pkg-config \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /src

COPY go.mod ./
COPY third_party/sendspin-go/go.mod third_party/sendspin-go/go.sum ./third_party/sendspin-go/
RUN go mod download

COPY . .

CMD ["go", "test", "./..."]
