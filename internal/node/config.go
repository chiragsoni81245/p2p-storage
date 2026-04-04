package node

import (
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/discovery"
)

type Config struct {
	ListenPort      int
	IdentityPath    string
	MinConnection   int
	MaxConnection   int
	Concurrency     int // Max concurrent request handling
	DiscoveryConfig discovery.Config

	// NAT Traversal config
	EnableRelay        bool          // Enable circuit relay client
	EnableHolePunch    bool          // Enable hole punching (DCUtR)
	EnableAutoNAT      bool          // Enable AutoNAT to detect NAT status
	RelayServers       []string      // Static relay server multiaddrs (your EC2 server)
	EnableRelayService bool          // Run as relay server (for your EC2 instance)
	HolePunchWait      time.Duration // How long to wait for hole punching (default: 10s)

	// Address announcement (for relay servers on EC2/cloud)
	ExternalIP string // Public IP to announce (e.g., "54.123.45.67")
}

func DefaultConfig() Config {
	return Config{
		ListenPort:           0,
		IdentityPath:         "./node.key",
		MinConnection:        50,
		MaxConnection:        100,
		Concurrency:          10,
		DiscoveryConfig:      discovery.DefaultConfig(),
		EnableRelay:        true,
		EnableHolePunch:    true,
		EnableAutoNAT:      true,
		RelayServers:       []string{},
		EnableRelayService: false,
		HolePunchWait:      10 * time.Second,
		ExternalIP:           "",
	}
}
