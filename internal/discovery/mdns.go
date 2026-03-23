package discovery

import (
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/chiragsoni81245/p2p-storage/internal/observability"
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
	Host   host.Host
	Bus    *event.Bus
	logger *observability.Logger
}

func (n *Notifee) HandlePeerFound(pi peer.AddrInfo) {
	// Ignore self
	if pi.ID == n.Host.ID() {
		return
	}

	n.logger.Debug("mDNS peer discovered", observability.Fields{
		"peer_id": pi.ID.String(),
		"addrs":   pi.Addrs,
	})

	n.Bus.Publish(event.Event{
		Type: event.PeerDiscovered,
		Data: PeerDiscoveredEvent{
			AddrInfo: pi,
		},
	})
}

// StartMDNS starts mDNS discovery with default configuration
func StartMDNS(h host.Host, bus *event.Bus, serviceName string, logger *observability.Logger) error {
	return StartMDNSWithConfig(h, bus, MDNSConfig{
		ServiceName: serviceName,
		Interval:    5 * time.Second,
	}, logger)
}

// StartMDNSWithConfig starts mDNS discovery with custom configuration
func StartMDNSWithConfig(h host.Host, bus *event.Bus, cfg MDNSConfig, logger *observability.Logger) error {
	svc := mdns.NewMdnsService(h, cfg.ServiceName, &Notifee{Host: h, Bus: bus, logger: logger})
	err := svc.Start()
	if err != nil {
		logger.Error("Failed to start mDNS service", observability.Fields{"error": err.Error()})
		return err
	}
	return nil
}
