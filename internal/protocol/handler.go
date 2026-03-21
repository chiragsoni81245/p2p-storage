package protocol

import (
	"context"

	"github.com/libp2p/go-libp2p/core/peer"
)

type Handler interface {
	Handle(ctx context.Context, peerID peer.ID, msg Message) (Message, error)
}
