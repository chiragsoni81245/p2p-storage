package network

import (
	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/multiformats/go-multiaddr"
)

type Notifier struct {
	bus *event.Bus
}

func NewNotifier(bus *event.Bus) *Notifier {
	return &Notifier{bus: bus}
}

func (n *Notifier) Connected(net network.Network, conn network.Conn) {
	n.bus.Publish(event.Event{
		Type: event.PeerConnected,
		Data: PeerEvent{
			PeerID: conn.RemotePeer(),
			Conn:   conn,
		},
	})
}

func (n *Notifier) Disconnected(net network.Network, conn network.Conn) {
	n.bus.Publish(event.Event{
		Type: event.PeerDisconnected,
		Data: PeerEvent{
			PeerID: conn.RemotePeer(),
			Conn:   conn,
		},
	})
}

// required no-op methods
func (n *Notifier) Listen(network.Network, multiaddr.Multiaddr)      {}
func (n *Notifier) ListenClose(network.Network, multiaddr.Multiaddr) {}
