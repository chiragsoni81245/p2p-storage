# p2p-storage

A peer-to-peer distributed file storage CLI built with [libp2p](https://libp2p.io/) in Go.

Store and retrieve files across a decentralized network of peers. Files are content-addressed using SHA256 hashes.

## Features

- **Content-addressed storage** - Files are identified by their SHA256 hash
- **AES-256 encryption** - Files encrypted at rest with streaming encryption
- **Multiple discovery methods** - mDNS (local), DHT (distributed), and bootstrap peers
- **Connection management** - Configurable connection limits with low/high watermarks
- **Rate limiting & backpressure** - Protects nodes from overload
- **Event-driven architecture** - Loosely coupled components
- **Persistent node identity** - Consistent peer ID across restarts

## Requirements

- Go 1.25.7+

## Installation

```bash
go mod download
go build -o p2p-storage ./cmd/node
```

## CLI Commands

### Store a file

```bash
p2p-storage store <filepath>

# Examples
p2p-storage store ./myfile.txt
p2p-storage store -s ./data ./document.pdf
```

Distributes the file to all connected peers and returns a key for retrieval.

### Retrieve a file

```bash
p2p-storage get <filekey> [output-path]

# Examples
p2p-storage get abc123...def ./output.txt
p2p-storage get abc123...def
```

Fetches a file from the network using its key. If available locally, returns immediately.

### Get file key

```bash
p2p-storage get-file-key <filepath>

# Example
p2p-storage get-file-key ./myfile.txt
```

Calculates the storage key for a file without storing it. Useful for scripting.

### Run as daemon

```bash
p2p-storage daemon

# Examples
p2p-storage -s ./data daemon
p2p-storage -d mdns,dht daemon
```

Starts the node as a background service that accepts connections and serves files.

### Global flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--config` | `-c` | | Path to config file |
| `--storage` | `-s` | `./storage` | Storage directory |
| `--wait` | `-w` | `5s` | Time to wait for peer discovery |
| `--timeout` | `-t` | `5m` | Operation timeout |
| `--log-file` | `-l` | stdout | Path to log file |
| `--discovery` | `-d` | `mdns` | Discovery methods (comma-separated: mdns, dht, bootstrap) |
| `--bootstrap` | `-b` | | Bootstrap peer addresses (multiaddr format) |

## Peer Discovery

The node supports multiple discovery methods that can be used together:

- **mDNS** - Automatic local network discovery (default)
- **DHT** - Kademlia distributed hash table for internet-wide discovery
- **Bootstrap** - Connect to known peers directly

```bash
# Local network only (default)
p2p-storage -d mdns daemon

# DHT for internet-wide discovery
p2p-storage -d dht daemon

# Connect to specific bootstrap peers
p2p-storage -b /ip4/192.168.1.100/tcp/4001/p2p/QmPeerID daemon

# Combine multiple methods
p2p-storage -d mdns,dht -b /ip4/1.2.3.4/tcp/4001/p2p/QmBootstrapPeer daemon
```

## Docker

Build the image:

```bash
docker build -t p2p-storage .
```

Run a daemon:

```bash
docker run --network host p2p-storage daemon
```

Store a file (with volume mount):

```bash
docker run --network host -v $(pwd):/data p2p-storage store /data/myfile.txt
```

## Configuration

Default settings in `internal/node/config.go`:

| Option | Default | Description |
|--------|---------|-------------|
| ListenPort | 0 (random) | TCP port to listen on |
| IdentityPath | ./node.key | Path to store node identity |
| MinConnection | 50 | Connection manager low watermark |
| MaxConnection | 100 | Connection manager high watermark |
| Concurrency | 10 | Max concurrent request handling |

Encryption settings in `internal/store/encryption.go`:

| Option | Default | Description |
|--------|---------|-------------|
| Enabled | true | Enable AES-256 encryption at rest |
| KeyPath | ./encryption.key | Path to encryption key file |

## Project Structure

```
cmd/node/           - CLI entry point (Cobra commands)
internal/
  config/           - Configuration aggregation
  core/             - Message handlers
  discovery/        - Peer discovery (mDNS, DHT, bootstrap)
  event/            - Event bus
  fileserver/       - File storage/retrieval logic
  middleware/       - Rate limiting, backpressure
  network/          - Connection management, peer scoring
  node/             - Node configuration and identity
  observability/    - Logging and metrics
  protocol/         - Protocol definitions (ping, file transfer)
  store/            - Content-addressed storage with encryption
```

## Testing

Run all tests:

```bash
go test ./...
```

Run unit tests:

```bash
go test -tags=unit ./...
```

Run integration tests:

```bash
go test -tags=integration ./...
```

Run with race detection:

```bash
go test -race ./...
```

Run with coverage:

```bash
go test -cover ./...
```