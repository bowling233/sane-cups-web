# Stage 1: Build pure Go binary
FROM golang:1.24-bookworm AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download
# Copy source code
COPY *.go ./

# Compile static Go binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o printer-web .

# Stage 2: Minimal runtime with SANE and CUPS client
FROM debian:trixie-slim

# Install sane-utils, sane-airscan, cups-client, and ca-certificates
RUN apt-get update && apt-get install -y --no-install-recommends \
    sane-utils \
    sane-airscan \
    cups-client \
    avahi-utils \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/printer-web /app/printer-web

# Copy static frontend assets
COPY static/ /app/static/

COPY config.example.yaml /app/config.yaml

# Create persistent scans directory
RUN mkdir -p /app/scans

EXPOSE 8085

ENV PORT=8085
# Compatibility with older embedded devices that generate X.509 certificates
# with a negative serial number. TLS verification settings still apply.
ENV GODEBUG=x509negativeserial=1

CMD ["/app/printer-web"]
