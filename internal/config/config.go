package config

import (
	"github.com/chiragsoni81245/p2p-storage/internal/middleware"
	"github.com/chiragsoni81245/p2p-storage/internal/node"
	"github.com/chiragsoni81245/p2p-storage/internal/protocol"
	"github.com/chiragsoni81245/p2p-storage/internal/store"
)

type Config struct {
	NodeConfig        node.Config
	RateLimiterConfig middleware.RateLimiterConfig
	ProtocolConfig    protocol.Config
	Encryption        store.EncryptionConfig
}
