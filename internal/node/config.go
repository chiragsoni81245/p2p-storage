package node

import "github.com/chiragsoni81245/p2p-storage/internal/discovery"

type Config struct {
	ListenPort      int
	IdentityPath    string
	MinConnection   int
	MaxConnection   int
	Concurrency     int // Max concurrent request handling
	DiscoveryConfig discovery.Config
}

func DefaultConfig() Config {
	return Config{
		ListenPort:      0,
		IdentityPath:    "./node.key",
		MinConnection:   50,
		MaxConnection:   100,
		Concurrency:     10,
		DiscoveryConfig: discovery.DefaultConfig(),
	}
}
