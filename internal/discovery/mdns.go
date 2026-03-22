package discovery

import (
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	mdns "github.com/libp2p/go-libp2p/p2p/discovery/mdns"
)

// MDNSConfig holds mDNS discovery configuration
type MDNSConfig struct {
	ServiceName string
	Interval    time.Duration // How often to query for peers
}

// DefaultMDNSConfig returns the default mDNS configuration
func DefaultMDNSConfig() MDNSConfig {
	return MDNSConfig{
		ServiceName: "storage",
		Interval:    5 * time.Second, // More aggressive discovery for macOS
	}
}

type Notifee struct {
	Host host.Host
	Bus  *event.Bus
}

func (n *Notifee) HandlePeerFound(pi peer.AddrInfo) {
	// Ignore self
	if pi.ID == n.Host.ID() {
		return
	}

	n.Bus.Publish(event.Event{
		Type: event.PeerDiscovered,
		Data: PeerDiscoveredEvent{
			AddrInfo: pi,
		},
	})
}

// StartMDNS starts mDNS discovery with default configuration
func StartMDNS(h host.Host, bus *event.Bus, serviceName string) error {
	return StartMDNSWithConfig(h, bus, MDNSConfig{
		ServiceName: serviceName,
		Interval:    5 * time.Second,
	})
}

// StartMDNSWithConfig starts mDNS discovery with custom configuration
func StartMDNSWithConfig(h host.Host, bus *event.Bus, cfg MDNSConfig) error {
	svc := mdns.NewMdnsService(h, cfg.ServiceName, &Notifee{Host: h, Bus: bus})
	return svc.Start()
}
