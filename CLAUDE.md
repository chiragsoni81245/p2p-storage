# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build
go build -o bin/p2p-storage ./cmd/node

# Run all tests
go test ./...

# Run tests with race detection
go test -race ./...

# Run a single package's tests
go test ./internal/fileserver/...

# Run a specific test
go test -run TestName ./internal/store/...

# Format and vet
go fmt ./...
go vet ./...

# Docker build
docker build -t p2p-storage .
```

## Architecture Overview

This is a peer-to-peer distributed file storage CLI built on [libp2p](https://libp2p.io/) in Go. Files are content-addressed by SHA256 hash, stored with AES-256 streaming encryption, and replicated across peers.

### Entry Point

`cmd/node/main.go` — Cobra CLI with five commands: `daemon`, `store`, `get`, `get-file-key`, `send`. Each command instantiates a `FileServer`, waits for peer discovery, executes the operation, then tears down.

### Core Components

**`internal/fileserver/`** — The central orchestrator. `FileServer` owns all subsystems, handles file store/retrieve operations, and coordinates peer replication. `transfer.go` implements the `/storage-transfer/1.0.0` protocol for direct bidirectional file streaming.

**`internal/node/node.go`** — Configures the libp2p host: transports (TCP/QUIC), security (Noise/TLS), connection limits, resource manager, relay, hole punching, and AutoNAT.

**`internal/store/store.go`** — Content-addressed storage with AES-256 encryption. Key → SHA1(key) → path on disk; file data streamed through AES in 64KB chunks with length prefixes.

**`internal/discovery/`** — Three discovery strategies orchestrated by `manager.go`: mDNS (local network), Kademlia DHT (internet-wide), and Bootstrap (known peers). Discovery events propagate through the event bus.

**`internal/protocol/protocol.go`** — Registers the `/storage/1.0.0` protocol, wraps handlers with rate limiting and backpressure middleware, enforces read/write timeouts.

**`internal/event/event.go`** — Lightweight pub/sub bus decoupling components. Key events: `PeerDiscovered`, `PeerConnected`, `PeerDisconnected`, `StoragePeerRegistered`, `StoragePeerUnregistered`, `FileTransferComplete`.

**`internal/network/`** — Peer scoring (`peerscore.go`) tracks success/failure rates with exponential decay (5-min half-life) to select the best peers for replication. `notifier.go` bridges libp2p connection notifications to the event bus.

**`internal/middleware/`** — `ratelimit.go` throttles per-peer requests; `backpressure.go` enforces slot-based concurrency limits.

**`internal/config/config.go`** — Three-tier config: CLI flags > YAML file > hardcoded defaults. `config.yaml` (gitignored) overrides `config.example.yaml`.

### Key Data Flows

**Storing a file:**
1. Compute SHA256 key from content
2. Initialize FileServer (all subsystems start)
3. Wait for peer discovery (configurable, default 5s)
4. Store locally in encrypted store
5. Replicate to connected peers via `/storage-transfer/1.0.0`

**Retrieving a file:**
1. Check local store first
2. If missing, query connected peers
3. Receive stream, write to output path

**Direct peer send (`send` command):**
1. Store locally, connect to target multiaddr
2. If relayed: wait for relay reservation, attempt hole punching
3. Transfer only if direct connection established (unless `--allow-relay` flag)

### Configuration

See `config.example.yaml` for all options. Key sections: `node` (ports, connection limits, relay/hole-punch toggles), `discovery` (enabled methods and their params), `protocol` (message size, timeouts), `rate_limiter`, `encryption`.

The node's persistent identity lives in `node.key`; the encryption key in `encryption.key` (both gitignored, mode 0600).
