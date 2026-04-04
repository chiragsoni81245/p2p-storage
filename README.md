# p2p-storage

A peer-to-peer distributed file storage CLI built with [libp2p](https://libp2p.io/) in Go.

Files are content-addressed using SHA256 hashes and stored locally with AES-256 encryption. Peers can request files from one another, and files can be sent directly to a specific peer.

## Features

- Content-addressed storage using SHA256 hashes
- AES-256 encryption at rest with streaming encryption
- Multiple discovery methods: mDNS (local), DHT (distributed), and bootstrap peers
- Hop-get: broadcast file requests propagate through the network; any node that has the file connects back and delivers it directly
- Direct peer-to-peer file transfer with NAT traversal via hole punching (DCUtR) through relay servers
- Session-based receive mode: only accept transfers from senders with the correct session name
- Daemon mode: run a propagation-only node that relays get requests and delivers files it holds
- Connection management with configurable limits
- Rate limiting and backpressure to protect nodes from overload
- Event-driven architecture with a pub/sub bus
- Persistent node identity across restarts

## Requirements

- Go 1.21+

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

Checks local storage first. If the file is not found locally, broadcasts a `GET_FILE` request into the network. Any peer that holds the file will connect back and deliver it directly. The command waits until delivery completes or you press Ctrl+C.

If relay servers are configured, the node waits for its relay reservation to appear in its address list before broadcasting, so the delivering peer can reach it even when behind NAT.

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
p2p-storage send ./myfile.txt /ip4/192.168.1.10/tcp/4001/p2p/12D3KooW...

# Through a relay — hole punching is attempted automatically to establish a direct connection
p2p-storage send ./myfile.txt /ip4/3.90.43.169/tcp/4001/p2p/12D3KooWRelay.../p2p-circuit/p2p/12D3KooWTarget...

# With a session name required by the receiver
p2p-storage send --session mysession ./myfile.txt /ip4/192.168.1.10/tcp/4001/p2p/12D3KooW...
```

Sends a file directly to a specific peer. The file is stored locally first, then transferred.

For relay circuit addresses (`/p2p-circuit`), the sender:
1. Connects to the relay
2. Waits for its own relay reservation so it is reachable back
3. Connects to the target peer through the relay
4. Attempts hole punching to upgrade to a direct connection

Only direct connections are used for the transfer. If hole punching fails and a direct connection cannot be established, the transfer is rejected with a clear error.

If the receiver is running `receive --session <name>`, supply the matching name via `--session` or the transfer will be rejected. If the receiver already has the file it will also be rejected.

### Run a propagation node (daemon)

```bash
p2p-storage daemon
```

Starts a node that participates in file discovery and propagation without initiating any transfers. It forwards `GET_FILE` requests to peers and delivers files it holds locally. Useful as an always-on relay or cache node. Press Ctrl+C to stop.

```
Node ID: 12D3KooW...
Address: /ip4/192.168.1.10/tcp/52341/p2p/12D3KooW...
Daemon running. Ctrl+C to stop.
[relay]    key=abc123def456... msgID=f3a1... ttl=2
[deliver]  key=abc123def456... requester=12D3KooW...
[done]     key=abc123def456... msgID=f3a1...
```

### Receive files from peers

```bash
p2p-storage receive --session <session-name>

# Example
p2p-storage receive --session mysession
```

Starts the node, prints its addresses, and waits for incoming file transfers. Only senders that supply the matching `--session` name are accepted. Progress for each incoming file is shown as a live progress bar. Press Ctrl+C to stop.

```
Node ID: 12D3KooW...
Address: /ip4/192.168.1.10/tcp/52341/p2p/12D3KooW...
Session: mysession
Waiting for incoming transfers... (Ctrl+C to stop)
```

Share one of the printed addresses with the sender so they can connect.

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
| `--session` | | Session name required by the receiver |

### Receive-specific flags

| Flag | Default | Description |
|------|---------|-------------|
| `--session` | (required) | Session name that senders must supply |

## NAT Traversal

When peers are behind NAT, direct connections require additional setup. The node supports two mechanisms, both configurable in `config.yaml`:

**Hole punching (DCUtR)** — After connecting through a relay, both peers simultaneously attempt a direct connection. This succeeds in most NAT configurations. Enabled by default (`enable_hole_punch: true`). The wait time is controlled by `hole_punch_wait` (default `10s`).

Transfers always require a direct connection — relay connections are only used as a signalling channel for hole punching, never for data transfer.

To use relay-based NAT traversal, add relay server addresses to your config:

```yaml
node:
  relay_servers:
    - /ip4/1.2.3.4/tcp/4001/p2p/12D3KooWRelayPeerID...
  enable_relay: true
  enable_hole_punch: true
  hole_punch_wait: 10s
```

## Peer Discovery

The node supports three discovery methods that can be combined:

- **mDNS** — Automatic local network discovery, enabled by default
- **DHT** — Kademlia distributed hash table for internet-wide discovery
- **Bootstrap** — Connect to known peers at startup

```bash
# Local network only (default)
p2p-storage get <key>

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
| `node.listen_port` | 0 (random) | TCP/QUIC port to listen on |
| `node.identity_path` | `./node.key` | Path to node identity key |
| `node.min_connection` | 50 | Connection manager low watermark |
| `node.max_connection` | 100 | Connection manager high watermark |
| `node.concurrency` | 10 | Max concurrent requests |
| `node.enable_relay` | true | Enable relay client for NAT traversal |
| `node.enable_hole_punch` | true | Enable hole punching (DCUtR) |
| `node.hole_punch_wait` | `10s` | Time to wait for hole punching |
| `encryption.enabled` | true | Enable AES-256 encryption at rest |
| `encryption.key_path` | `./encryption.key` | Path to encryption key |

## Project Structure

```
cmd/node/           - CLI entry point (Cobra commands)
pkg/
  operations/       - Reusable operations (store, get, send, receive) for CLI and web use
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
