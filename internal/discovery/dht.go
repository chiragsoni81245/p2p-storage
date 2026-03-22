package discovery

import (
	"context"
	"sync"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/event"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
)

// DHTConfig holds DHT discovery configuration
type DHTConfig struct {
	// RendezvousString is the namespace for peer discovery
	RendezvousString string
	// DiscoveryInterval is how often to search for new peers
	DiscoveryInterval time.Duration
	// BootstrapPeers are initial DHT peers to connect to
	BootstrapPeers []peer.AddrInfo
	// Mode can be "server" (full DHT) or "client" (uses other nodes)
	Mode string
}

// DefaultDHTConfig returns the default DHT configuration
func DefaultDHTConfig() DHTConfig {
	return DHTConfig{
		RendezvousString:  "p2p-storage-network",
		DiscoveryInterval: 10 * time.Second,
		BootstrapPeers:    dht.GetDefaultBootstrapPeerAddrInfos(),
		Mode:              "auto", // auto-detect based on network conditions
	}
}

// DHTDiscovery handles DHT-based peer discovery
type DHTDiscovery struct {
	host      host.Host
	bus       *event.Bus
	config    DHTConfig
	dht       *dht.IpfsDHT
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.RWMutex
	running   bool
	seenPeers map[peer.ID]time.Time
}

// NewDHTDiscovery creates a new DHT discovery service
func NewDHTDiscovery(h host.Host, bus *event.Bus, cfg DHTConfig) *DHTDiscovery {
	ctx, cancel := context.WithCancel(context.Background())
	return &DHTDiscovery{
		host:      h,
		bus:       bus,
		config:    cfg,
		ctx:       ctx,
		cancel:    cancel,
		seenPeers: make(map[peer.ID]time.Time),
	}
}

// Start initializes and starts DHT discovery
func (d *DHTDiscovery) Start() error {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return nil
	}
	d.mu.Unlock()

	// Create the DHT
	var opts []dht.Option
	switch d.config.Mode {
	case "server":
		opts = append(opts, dht.Mode(dht.ModeServer))
	case "client":
		opts = append(opts, dht.Mode(dht.ModeClient))
	default:
		opts = append(opts, dht.Mode(dht.ModeAutoServer))
	}

	kademliaDHT, err := dht.New(d.ctx, d.host, opts...)
	if err != nil {
		return err
	}
	d.dht = kademliaDHT

	// Bootstrap the DHT
	if err := d.dht.Bootstrap(d.ctx); err != nil {
		return err
	}

	// Connect to bootstrap peers
	d.connectToBootstrapPeers()

	d.mu.Lock()
	d.running = true
	d.mu.Unlock()

	// Start peer discovery loop
	go d.discoveryLoop()

	return nil
}

// Stop stops the DHT discovery service
func (d *DHTDiscovery) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.cancel()
	d.running = false

	if d.dht != nil {
		return d.dht.Close()
	}
	return nil
}

func (d *DHTDiscovery) connectToBootstrapPeers() {
	var wg sync.WaitGroup

	for _, peerAddr := range d.config.BootstrapPeers {
		if peerAddr.ID == d.host.ID() {
			continue // Skip self
		}

		wg.Add(1)
		go func(pi peer.AddrInfo) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(d.ctx, 30*time.Second)
			defer cancel()

			if err := d.host.Connect(ctx, pi); err == nil {
				d.notifyPeerDiscovered(pi)
			}
		}(peerAddr)
	}

	wg.Wait()
}

func (d *DHTDiscovery) discoveryLoop() {
	// Create a routing discovery service
	routingDiscovery := drouting.NewRoutingDiscovery(d.dht)

	// Advertise ourselves
	dutil.Advertise(d.ctx, routingDiscovery, d.config.RendezvousString)

	ticker := time.NewTicker(d.config.DiscoveryInterval)
	defer ticker.Stop()

	// Run initial discovery
	d.findPeers(routingDiscovery)

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.findPeers(routingDiscovery)
		}
	}
}

func (d *DHTDiscovery) findPeers(routingDiscovery *drouting.RoutingDiscovery) {
	ctx, cancel := context.WithTimeout(d.ctx, d.config.DiscoveryInterval)
	defer cancel()

	peerChan, err := routingDiscovery.FindPeers(ctx, d.config.RendezvousString)
	if err != nil {
		return
	}

	for p := range peerChan {
		if p.ID == d.host.ID() || len(p.Addrs) == 0 {
			continue
		}

		// Check if we've seen this peer recently
		d.mu.RLock()
		lastSeen, seen := d.seenPeers[p.ID]
		d.mu.RUnlock()

		if seen && time.Since(lastSeen) < d.config.DiscoveryInterval*2 {
			continue // Skip recently seen peers
		}

		// Try to connect
		connectCtx, connectCancel := context.WithTimeout(d.ctx, 10*time.Second)
		if err := d.host.Connect(connectCtx, p); err == nil {
			d.notifyPeerDiscovered(p)
		}
		connectCancel()
	}
}

func (d *DHTDiscovery) notifyPeerDiscovered(pi peer.AddrInfo) {
	d.mu.Lock()
	d.seenPeers[pi.ID] = time.Now()
	d.mu.Unlock()

	d.bus.Publish(event.Event{
		Type: event.PeerDiscovered,
		Data: PeerDiscoveredEvent{
			AddrInfo: pi,
		},
	})
}

// GetDHT returns the underlying DHT (useful for content routing)
func (d *DHTDiscovery) GetDHT() *dht.IpfsDHT {
	return d.dht
}

// FindPeer attempts to find a specific peer in the DHT
func (d *DHTDiscovery) FindPeer(id peer.ID) (peer.AddrInfo, error) {
	ctx, cancel := context.WithTimeout(d.ctx, 30*time.Second)
	defer cancel()
	return d.dht.FindPeer(ctx, id)
}
