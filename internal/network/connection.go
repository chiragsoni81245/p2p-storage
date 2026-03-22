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

	go m.listen()

	return m
}

func (m *Manager) listen() {
	ch := m.bus.Subscribe(event.PeerDiscovered)

	for evt := range ch {
		event := evt.Data.(discovery.PeerDiscoveredEvent)

		go m.connect(event.AddrInfo)
	}
}

func (m *Manager) connect(pi peer.AddrInfo) {
	if m.host.Network().Connectedness(pi.ID) == network.Connected {
		// Ignore if already connected
		return
	}

	if len(m.host.Network().Peers()) >= m.maxConnection {
		/*
			Stop connecting peers if we already have max connections
			This is required for admission control as prune will happen after connection cost is paid
			so for sudden big spike this is better prevention
		*/
		return
	}

	err := m.host.Connect(context.Background(), pi)
	if err != nil {
		m.logger.Error("connection failed", observability.Fields{
			"error": err.Error(),
		})
		return
	}
}
