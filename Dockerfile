# build executable
FROM golang:1.25-trixie@sha256:4f9d98ebaa759f776496d850e0439c48948d587b191fc3949b5f5e4667abef90 AS builder

WORKDIR /build

# Copy and download dependencies using go mod
COPY go.mod go.sum ./
RUN go mod download

# Copy the code into the container
COPY . ./

# Build the applications
RUN go build -o /app/flare_cchain_indexer ./main.go

FROM debian:trixie@sha256:72547dd722cd005a8c2aa2079af9ca0ee93aad8e589689135feaed60b0a8c08d AS execution

WORKDIR /app

COPY --from=builder /app/flare_cchain_indexer .
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

CMD ["./flare_cchain_indexer"]
