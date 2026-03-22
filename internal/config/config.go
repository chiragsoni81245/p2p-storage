package config

import (
	"github.com/chiragsoni81245/p2p-storage/internal/middleware"
	"github.com/chiragsoni81245/p2p-storage/internal/node"
	"github.com/chiragsoni81245/p2p-storage/internal/protocol"
)

type Config struct {
	NodeConfig        node.Config
	RateLimiterConfig middleware.RateLimiterConfig
	ProtocolConfig    protocol.Config
}
