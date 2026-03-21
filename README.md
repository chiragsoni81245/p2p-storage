# p2p-storage

A peer-to-peer storage network built with [libp2p](https://libp2p.io/) in Go.

## Features

- Peer discovery via mDNS
- Connection management with configurable limits
- Rate limiting and backpressure middleware
- Peer scoring system
- Event-driven architecture
- Persistent node identity

## Requirements

- Go 1.25+

## Installation

```bash
go mod download
```

## Usage

```bash
go run cmd/node/main.go
```

## Docker

Build the image:

```bash
docker build -t p2p-storage .
```

Run a node:

```bash
docker run --network host p2p-storage
```

## Configuration

Default settings in `internal/node/config.go`:

| Option | Default | Description |
|--------|---------|-------------|
| ListenPort | 0 (random) | TCP port to listen on |
| IdentityPath | ./node.key | Path to store node identity |
| MinConnection | 50 | Connection manager low watermark |
| MaxConnection | 100 | Connection manager high watermark |

## Project Structure

```
cmd/node/         - Application entry point
internal/
  core/           - Message handlers
  discovery/      - Peer discovery (mDNS)
  event/          - Event bus
  middleware/     - Rate limiting, backpressure
  network/        - Connection management, peer scoring
  node/           - Node configuration and setup
  observability/  - Logging and metrics
  protocol/       - Protocol definitions
pkg/logger/       - Logger utilities
```