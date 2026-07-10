# build executable
FROM golang:1.25-trixie@sha256:75eb6e343bdac854462811220a5bcae3442af3fb25e8c8644c7494e5e5545aed AS builder

WORKDIR /build

# Copy and download dependencies using go mod
COPY go.mod go.sum ./
RUN go mod download

# Copy the code into the container
COPY . ./

# Build the applications
RUN go build -o /app/flare-cchain-indexer ./cmd/indexer

FROM debian:trixie-slim@sha256:cedb1ef40439206b673ee8b33a46a03a0c9fa90bf3732f54704f99cb061d2c5a AS execution

# ca-certificates: outbound HTTPS (RPC endpoints, etc.).
# wget: docker healthcheck against /health on port 8080.
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates wget \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /app/flare-cchain-indexer .

CMD ["./flare-cchain-indexer"]
