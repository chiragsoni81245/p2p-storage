package node

import (
	"fmt"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	connmgr "github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	ma "github.com/multiformats/go-multiaddr"
)

func NewNode(cfg Config) (host.Host, error) {
	cm, err := connmgr.NewConnManager(
		cfg.MinConnection, // low watermark
		cfg.MaxConnection, // high watermark
	)

	if err != nil {
		return nil, err
	}

	priv, err := LoadOrCreateIdentity(cfg.IdentityPath)
	if err != nil {
		return nil, err
	}

	// Determine the port for address construction
	port := cfg.ListenPort
	if port == 0 {
		port = 4001 // default port for announce addresses when random port is used
	}

	// Build libp2p options
	opts := []libp2p.Option{
		libp2p.Identity(priv),

		libp2p.ListenAddrStrings(
			fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", cfg.ListenPort),
			fmt.Sprintf("/ip4/0.0.0.0/udp/%d/quic-v1", cfg.ListenPort), // QUIC for better hole punching
		),

		// Default stack
		libp2p.DefaultTransports,
		libp2p.DefaultSecurity,
		libp2p.DefaultMuxers,

		// Conn Manager
		libp2p.ConnectionManager(cm),
	}

	// Configure announce addresses (for EC2/cloud instances with public IP)
	// Only ExternalIP is allowed - peer ID is added automatically by libp2p
	if cfg.ExternalIP != "" {
		tcpAddr, _ := ma.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/%d", cfg.ExternalIP, port))
		quicAddr, _ := ma.NewMultiaddr(fmt.Sprintf("/ip4/%s/udp/%d/quic-v1", cfg.ExternalIP, port))
		opts = append(opts, libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
			return append(addrs, tcpAddr, quicAddr)
		}))
	}

	// Enable relay client (allows this node to connect via relay servers)
	if cfg.EnableRelay {
		opts = append(opts, libp2p.EnableRelay())
	}

	// Enable hole punching (DCUtR - Direct Connection Upgrade through Relay)
	if cfg.EnableHolePunch {
		opts = append(opts, libp2p.EnableHolePunching())
	}

	// Enable AutoNAT to detect if we're behind NAT
	if cfg.EnableAutoNAT {
		opts = append(opts, libp2p.EnableAutoNATv2())
	}

	// Configure static relay servers (your EC2 relay)
	if len(cfg.RelayServers) > 0 {
		var relayInfos []peer.AddrInfo
		for _, addrStr := range cfg.RelayServers {
			maddr, err := ma.NewMultiaddr(addrStr)
			if err != nil {
				return nil, fmt.Errorf("invalid relay address %s: %w", addrStr, err)
			}
			info, err := peer.AddrInfoFromP2pAddr(maddr)
			if err != nil {
				return nil, fmt.Errorf("failed to parse relay peer info: %w", err)
			}
			relayInfos = append(relayInfos, *info)
		}
		opts = append(opts, libp2p.EnableAutoRelayWithStaticRelays(relayInfos))
	}

	node, err := libp2p.New(opts...)
	if err != nil {
		return nil, err
	}

	// If this node should act as a relay server (for EC2 instance)
	if cfg.EnableRelayService {
		_, err = relay.New(node)
		if err != nil {
			node.Close()
			return nil, fmt.Errorf("failed to start relay service: %w", err)
		}
	}

	return node, nil
}
