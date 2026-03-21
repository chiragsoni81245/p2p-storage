package discovery

import "github.com/libp2p/go-libp2p/core/peer"

type PeerDiscoveredEvent struct {
	AddrInfo peer.AddrInfo
}
