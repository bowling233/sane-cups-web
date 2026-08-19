# Stage 1: Build pure Go binary
FROM golang:1.24-bookworm AS builder

WORKDIR /app

COPY go.mod ./
# Copy source code
COPY main.go ./

# Compile static Go binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o printer-web ./main.go

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

# Create persistent scans directory
RUN mkdir -p /app/scans

EXPOSE 8085

ENV PORT=8085

CMD ["/app/printer-web"]
