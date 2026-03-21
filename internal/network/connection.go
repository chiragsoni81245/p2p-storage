package network

import (
	"context"
	"fmt"

	"github.com/chiragsoni81245/p2p-storage/internal/event"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

type Manager struct {
	host host.Host
	bus  *event.Bus
}

func NewManager(h host.Host, bus *event.Bus) *Manager {
	m := &Manager{
		host: h,
		bus:  bus,
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
