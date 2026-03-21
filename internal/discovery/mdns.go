package discovery

import (
	"fmt"

	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	mdns "github.com/libp2p/go-libp2p/p2p/discovery/mdns"
)

type Notifee struct {
	Host host.Host
	Bus *event.Bus
}

func (n *Notifee) HandlePeerFound(pi peer.AddrInfo) {
	// 🚫 Ignore self
	if pi.ID == n.Host.ID() {
		return
	}

	fmt.Println("Discovered peer:", pi.ID)

	n.Bus.Publish(event.Event{
		Type: event.PeerDiscovered,
		Data: pi,
	})
}

func StartMDNS(h host.Host, bus *event.Bus, serviceName string) error {
	svc := mdns.NewMdnsService(h, serviceName, &Notifee{Host: h, Bus: bus})
	return svc.Start()
}
