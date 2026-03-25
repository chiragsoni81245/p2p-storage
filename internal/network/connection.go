package network

import (
	"context"

	"github.com/chiragsoni81245/p2p-storage/internal/discovery"
	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/chiragsoni81245/p2p-storage/internal/observability"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

type Manager struct {
	host   host.Host
	bus    *event.Bus
	logger *observability.Logger
}

func NewManager(logger *observability.Logger, h host.Host, bus *event.Bus) *Manager {
	m := &Manager{
		host:   h,
		bus:    bus,
		logger: logger,
	}

	h.Network().Notify(NewNotifier(bus))

	// Subscribe to peer discovery events
	discoveryCh := bus.Subscribe(event.PeerDiscovered)
	logger.Info("network manager subscribed to peer discovery events", observability.Fields{})

	go m.listen(discoveryCh)

	return m
}

func (m *Manager) listen(ch <-chan event.Event) {
	m.logger.Info("network manager starting to listen for discovered peers", observability.Fields{})

	for evt := range ch {
		event := evt.Data.(discovery.PeerDiscoveredEvent)
		m.logger.Info("received peer discovered event", observability.Fields{
			"peer_id": event.AddrInfo.ID.String(),
			"addrs":   event.AddrInfo.Addrs,
		})

		go m.connect(event.AddrInfo)
	}
}

func (m *Manager) connect(pi peer.AddrInfo) {
	m.logger.Info("attempting to connect to discovered peer", observability.Fields{
		"peer_id": pi.ID.String(),
		"addrs":   pi.Addrs,
	})

	if m.host.Network().Connectedness(pi.ID) == network.Connected {
		m.logger.Info("peer already connected, skipping", observability.Fields{
			"peer_id": pi.ID.String(),
		})
		return
	}

	err := m.host.Connect(context.Background(), pi)
	if err != nil {
		// Connection may fail due to ResourceManager limits, which is expected
		m.logger.Error("connection failed", observability.Fields{
			"peer_id": pi.ID.String(),
			"addrs":   pi.Addrs,
			"error":   err.Error(),
		})
		return
	}

	m.logger.Info("successfully connected to peer", observability.Fields{
		"peer_id": pi.ID.String(),
	})
}
