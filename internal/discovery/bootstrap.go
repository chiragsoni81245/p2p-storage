package discovery

import (
	"context"
	"sync"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/event"
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
	ctx    context.Context
	cancel context.CancelFunc
}

// NewBootstrap creates a new bootstrap discovery service
func NewBootstrap(h host.Host, bus *event.Bus, cfg BootstrapConfig) *Bootstrap {
	ctx, cancel := context.WithCancel(context.Background())
	return &Bootstrap{
		host:   h,
		bus:    bus,
		config: cfg,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start initiates connections to bootstrap peers
func (b *Bootstrap) Start() error {
	if len(b.config.BootstrapPeers) == 0 {
		return nil // No bootstrap peers configured
	}

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
	maddr, err := multiaddr.NewMultiaddr(addrStr)
	if err != nil {
		return
	}

	peerInfo, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return
	}

	// Skip self
	if peerInfo.ID == b.host.ID() {
		return
	}

	for attempt := 0; attempt <= b.config.MaxRetries; attempt++ {
		select {
		case <-b.ctx.Done():
			return
		default:
		}

		ctx, cancel := context.WithTimeout(b.ctx, b.config.ConnectionTimeout)
		err := b.host.Connect(ctx, *peerInfo)
		cancel()

		if err == nil {
			// Successfully connected, publish event
			b.bus.Publish(event.Event{
				Type: event.PeerDiscovered,
				Data: PeerDiscoveredEvent{
					AddrInfo: *peerInfo,
				},
			})
			return
		}

		// Wait before retry
		if attempt < b.config.MaxRetries {
			select {
			case <-b.ctx.Done():
				return
			case <-time.After(b.config.RetryInterval):
			}
		}
	}
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
