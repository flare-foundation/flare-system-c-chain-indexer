# build executable
FROM golang:1.24-trixie@sha256:73860edd0febb45d37a5220ba39a6675d67f72163c79a4cebd9d0e6985609140 AS builder

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
