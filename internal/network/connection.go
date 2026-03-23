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
	host          host.Host
	bus           *event.Bus
	maxConnection int
	logger        *observability.Logger
}

func NewManager(maxConnection int, logger *observability.Logger, h host.Host, bus *event.Bus) *Manager {
	m := &Manager{
		host:          h,
		bus:           bus,
		maxConnection: maxConnection,
		logger:        logger,
	}

	h.Network().Notify(NewNotifier(bus))

	// Subscribe BEFORE returning to avoid race condition with discovery
	// The subscription must be ready before discovery starts publishing events
	ch := bus.Subscribe(event.PeerDiscovered)
	logger.Info("network manager subscribed to peer discovery events", observability.Fields{})

	go m.listen(ch)

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

	if len(m.host.Network().Peers()) >= m.maxConnection {
		m.logger.Info("max connections reached, skipping peer", observability.Fields{
			"peer_id":        pi.ID.String(),
			"current_peers":  len(m.host.Network().Peers()),
			"max_connection": m.maxConnection,
		})
		return
	}

	err := m.host.Connect(context.Background(), pi)
	if err != nil {
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
