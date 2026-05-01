# build executable
FROM golang:1.25-trixie@sha256:4f9d98ebaa759f776496d850e0439c48948d587b191fc3949b5f5e4667abef90 AS builder

WORKDIR /build

# Copy and download dependencies using go mod
COPY go.mod go.sum ./
RUN go mod download

# Copy the code into the container
COPY . ./

# Build the applications
RUN go build -o /app/flare_cchain_indexer ./cmd/indexer

FROM debian:trixie-slim@sha256:cedb1ef40439206b673ee8b33a46a03a0c9fa90bf3732f54704f99cb061d2c5a AS execution

# ca-certificates: outbound HTTPS (RPC endpoints, etc.).
# wget: docker healthcheck against /health on port 8080.
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates wget \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /app/flare_cchain_indexer .

CMD ["./flare_cchain_indexer"]
