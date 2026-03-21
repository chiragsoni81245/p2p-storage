package network

import (
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

type PeerEvent struct {
	PeerID peer.ID
	Conn   network.Conn
}
