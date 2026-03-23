package discovery

import (
	"context"
	"sync"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/chiragsoni81245/p2p-storage/internal/observability"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

// BootstrapConfig holds bootstrap peer discovery configuration
type BootstrapConfig struct {
	// BootstrapPeers is a list of multiaddrs for initial peer connections
	BootstrapPeers []string
	// ConnectionTimeout for connecting to each bootstrap peer
	ConnectionTimeout time.Duration
	// RetryInterval for retrying failed connections
	RetryInterval time.Duration
	// MaxRetries maximum number of retries per peer
	MaxRetries int
}

// DefaultBootstrapConfig returns the default bootstrap configuration
func DefaultBootstrapConfig() BootstrapConfig {
	return BootstrapConfig{
		BootstrapPeers:    []string{},
		ConnectionTimeout: 30 * time.Second,
		RetryInterval:     10 * time.Second,
		MaxRetries:        3,
	}
}

// Bootstrap handles connecting to known bootstrap peers
type Bootstrap struct {
	host   host.Host
	bus    *event.Bus
	config BootstrapConfig
	logger *observability.Logger
	ctx    context.Context
	cancel context.CancelFunc
}

// NewBootstrap creates a new bootstrap discovery service
func NewBootstrap(h host.Host, bus *event.Bus, cfg BootstrapConfig, logger *observability.Logger) *Bootstrap {
	ctx, cancel := context.WithCancel(context.Background())
	return &Bootstrap{
		host:   h,
		bus:    bus,
		config: cfg,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start initiates connections to bootstrap peers
func (b *Bootstrap) Start() error {
	if len(b.config.BootstrapPeers) == 0 {
		b.logger.Info("bootstrap: no peers configured", observability.Fields{})
		return nil
	}

	b.logger.Info("bootstrap: starting with peers", observability.Fields{
		"peer_count": len(b.config.BootstrapPeers),
		"peers":      b.config.BootstrapPeers,
	})

	go b.connectToBootstrapPeers()
	return nil
}

// Stop stops the bootstrap service
func (b *Bootstrap) Stop() {
	b.cancel()
}

func (b *Bootstrap) connectToBootstrapPeers() {
	var wg sync.WaitGroup

	for _, addrStr := range b.config.BootstrapPeers {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			b.connectWithRetry(addr)
		}(addrStr)
	}

	wg.Wait()
}

func (b *Bootstrap) connectWithRetry(addrStr string) {
	b.logger.Info("bootstrap: parsing peer address", observability.Fields{
		"addr": addrStr,
	})

	maddr, err := multiaddr.NewMultiaddr(addrStr)
	if err != nil {
		b.logger.Error("bootstrap: invalid multiaddr", observability.Fields{
			"addr":  addrStr,
			"error": err.Error(),
		})
		return
	}

	peerInfo, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		b.logger.Error("bootstrap: failed to parse peer info", observability.Fields{
			"addr":  addrStr,
			"error": err.Error(),
		})
		return
	}

	// Skip self
	if peerInfo.ID == b.host.ID() {
		b.logger.Info("bootstrap: skipping self", observability.Fields{
			"peer_id": peerInfo.ID.String(),
		})
		return
	}

	b.logger.Info("bootstrap: connecting to peer", observability.Fields{
		"peer_id": peerInfo.ID.String(),
		"addrs":   peerInfo.Addrs,
	})

	for attempt := 0; attempt <= b.config.MaxRetries; attempt++ {
		select {
		case <-b.ctx.Done():
			return
		default:
		}

		b.logger.Info("bootstrap: connection attempt", observability.Fields{
			"peer_id": peerInfo.ID.String(),
			"attempt": attempt + 1,
			"max":     b.config.MaxRetries + 1,
		})

		ctx, cancel := context.WithTimeout(b.ctx, b.config.ConnectionTimeout)
		err := b.host.Connect(ctx, *peerInfo)
		cancel()

		if err == nil {
			b.logger.Info("bootstrap: connected successfully", observability.Fields{
				"peer_id": peerInfo.ID.String(),
			})
			// Successfully connected, publish event
			b.bus.Publish(event.Event{
				Type: event.PeerDiscovered,
				Data: PeerDiscoveredEvent{
					AddrInfo: *peerInfo,
				},
			})
			return
		}

		b.logger.Error("bootstrap: connection failed", observability.Fields{
			"peer_id": peerInfo.ID.String(),
			"attempt": attempt + 1,
			"error":   err.Error(),
		})

		// Wait before retry
		if attempt < b.config.MaxRetries {
			b.logger.Info("bootstrap: retrying in", observability.Fields{
				"peer_id":  peerInfo.ID.String(),
				"interval": b.config.RetryInterval.String(),
			})
			select {
			case <-b.ctx.Done():
				return
			case <-time.After(b.config.RetryInterval):
			}
		}
	}

	b.logger.Error("bootstrap: all retries exhausted", observability.Fields{
		"peer_id": peerInfo.ID.String(),
	})
}

// AddBootstrapPeer dynamically adds a new bootstrap peer
func (b *Bootstrap) AddBootstrapPeer(addrStr string) error {
	maddr, err := multiaddr.NewMultiaddr(addrStr)
	if err != nil {
		return err
	}

	peerInfo, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return err
	}

	if peerInfo.ID == b.host.ID() {
		return nil // Skip self
	}

	go b.connectWithRetry(addrStr)
	return nil
}
