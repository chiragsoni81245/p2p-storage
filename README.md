# p2p-storage

A peer-to-peer distributed file storage CLI built with [libp2p](https://libp2p.io/) in Go.

Files are content-addressed using SHA256 hashes and stored locally with AES-256 encryption. Peers can request files from one another, and files can be sent directly to a specific peer.

## Features

- Content-addressed storage using SHA256 hashes
- AES-256 encryption at rest with streaming encryption
- Multiple discovery methods: mDNS (local), DHT (distributed), and bootstrap peers
- Direct peer-to-peer file transfer with NAT hole punching and relay fallback
- Connection management with configurable limits
- Rate limiting and backpressure to protect nodes from overload
- Event-driven architecture with a pub/sub bus
- Persistent node identity across restarts

## Requirements

- Go 1.25.7+

## Installation

```bash
go mod download
go build -o p2p-storage ./cmd/node
```

## CLI Commands

### Store a file locally

```bash
p2p-storage store <filepath>

# Example
p2p-storage store ./myfile.txt
```

Stores the file in this node's local storage and prints the key. The file is not sent to any peers.

### Retrieve a file

```bash
p2p-storage get <filekey> [output-path]

# Examples
p2p-storage get abc123...def ./output.txt
p2p-storage get abc123...def
```

Checks local storage first. If the file is not found locally, requests it from connected peers and saves it to local storage, then writes it to the output path.

### Get file key

```bash
p2p-storage get-file-key <filepath>

# Example
p2p-storage get-file-key ./myfile.txt
```

Calculates the storage key for a file without storing it. Useful for scripting.

### Send a file to a peer

```bash
p2p-storage send <filepath> <peer-multiaddr>

# Direct connection
p2p-storage send ./myfile.txt /ip4/3.90.43.169/tcp/9999/p2p/12D3KooWKncG1bn23yiLDM8fhEmCQMbCgdXmsBrYJP3hcZkeUauy

# Through a relay with hole punching (peer must already be registered with the relay)
p2p-storage send ./myfile.txt /ip4/3.90.43.169/tcp/4001/p2p/12D3KooWJ7Q8u2KvMD1XfkhctUfgsNvDFd9RVJwjtKwJbj74kUaC/p2p-circuit/p2p/12D3KooWKncG1bn23yiLDM8fhEmCQMbCgdXmsBrYJP3hcZkeUauy

# Through a relay without attempting a direct connection (not recommended for sensitive data)
p2p-storage send --allow-relay ./myfile.txt /ip4/3.90.43.169/tcp/4001/p2p/12D3KooWJ7Q8u2KvMD1XfkhctUfgsNvDFd9RVJwjtKwJbj74kUaC/p2p-circuit/p2p/12D3KooWKncG1bn23yiLDM8fhEmCQMbCgdXmsBrYJP3hcZkeUauy
```

Sends a file directly to a specific peer. The file is stored locally first, then transferred. By default only a direct connection is used; if both peers are behind NAT, hole punching is attempted. Use `--allow-relay` to permit transfer over a relayed connection.

### Global flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--config` | `-c` | `./config.yaml` | Path to config file |
| `--storage` | `-s` | `./storage` | Storage directory |
| `--wait` | `-w` | `5s` | Time to wait for peer discovery |
| `--timeout` | `-t` | `5m` | Operation timeout |
| `--log-file` | `-l` | stdout | Path to log file |
| `--log-level` | | `info` | Log level: debug, info, error |
| `--discovery` | `-d` | `mdns` | Discovery methods (comma-separated: mdns, dht, bootstrap) |
| `--bootstrap` | `-b` | | Bootstrap peer addresses in multiaddr format |

### Send-specific flags

| Flag | Default | Description |
|------|---------|-------------|
| `--allow-relay` | false | Allow transfer over a relayed connection |
| `--hole-punch-wait` | `10s` | Time to wait for hole punching before giving up |

## Peer Discovery

The node supports three discovery methods that can be combined:

- **mDNS** - Automatic local network discovery, enabled by default
- **DHT** - Kademlia distributed hash table for internet-wide discovery
- **Bootstrap** - Connect to known peers at startup

```bash
# Local network only (default)
p2p-storage -d mdns get <key>

# DHT for internet-wide discovery
p2p-storage -d dht get <key>

# Connect to specific bootstrap peers
p2p-storage -b /ip4/192.168.1.100/tcp/4001/p2p/QmPeerID get <key>

# Combine multiple methods
p2p-storage -d mdns,dht -b /ip4/1.2.3.4/tcp/4001/p2p/QmBootstrapPeer get <key>
```

## Docker

Build the image:

```bash
docker build -t p2p-storage .
```

Store a file:

```bash
docker run --network host -v $(pwd):/data p2p-storage store /data/myfile.txt
```

Retrieve a file:

```bash
docker run --network host -v $(pwd):/data p2p-storage get <key> /data/output.txt
```

## Configuration

See `config.example.yaml` for all available options. The config file is loaded from `./config.yaml` by default; CLI flags take precedence over file values.

Key defaults:

| Option | Default | Description |
|--------|---------|-------------|
| `node.listen_port` | 0 (random) | TCP port to listen on |
| `node.identity_path` | `./node.key` | Path to node identity key |
| `node.min_connection` | 50 | Connection manager low watermark |
| `node.max_connection` | 100 | Connection manager high watermark |
| `node.concurrency` | 10 | Max concurrent requests |
| `encryption.enabled` | true | Enable AES-256 encryption at rest |
| `encryption.key_path` | `./encryption.key` | Path to encryption key |

## Project Structure

```
cmd/node/           - CLI entry point (Cobra commands)
pkg/
  operations/       - Reusable operations (store, get, send) for CLI and web use
internal/
  config/           - Configuration loading and merging
  core/             - Message handler interface
  discovery/        - Peer discovery (mDNS, DHT, bootstrap)
  event/            - Pub/sub event bus
  fileserver/       - File storage and retrieval orchestration
  middleware/        - Rate limiting and backpressure
  network/          - Connection management and peer scoring
  node/             - libp2p host configuration and identity
  observability/    - Logging and metrics
  protocol/         - Protocol definitions and message framing
  store/            - Content-addressed storage with AES-256 encryption
```

## Testing

```bash
# Run all tests
go test ./...

# Run with race detection
go test -race ./...
```
