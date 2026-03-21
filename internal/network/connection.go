package network

import (
	"context"
	"fmt"

	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/chiragsoni81245/p2p-storage/internal/node"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

type Manager struct {
	host host.Host
	bus  *event.Bus
	cfg  node.Config
}

func NewManager(cfg node.Config, h host.Host, bus *event.Bus) *Manager {
	m := &Manager{
		host: h,
		bus:  bus,
		cfg:  cfg,
	}

	go m.listen()

	return m
}

func (m *Manager) listen() {
	ch := m.bus.Subscribe(event.PeerDiscovered)

	for evt := range ch {
		pi := evt.Data.(peer.AddrInfo)

		go m.connect(pi)
	}
}

func (m *Manager) connect(pi peer.AddrInfo) {
	if m.host.Network().Connectedness(pi.ID) == network.Connected {
		// Ignore if already connected
		return
	}

	if len(m.host.Network().Peers()) >= m.cfg.MaxConnection {
		/*
			Stop connecting peers if we already have max connections
			This is required for admission control as prune will happen after connection cost is paid 
			so for sudden big spike this is better prevention
		*/
		return
	}

	fmt.Println("Connecting to:", pi.ID)

	err := m.host.Connect(context.Background(), pi)
	if err != nil {
		fmt.Println("Connection failed:", err)
		return
	}

	fmt.Println("Connected to:", pi.ID)

	m.bus.Publish(event.Event{
		Type: event.PeerConnected,
		Data: pi,
	})
}
