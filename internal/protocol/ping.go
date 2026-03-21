package protocol

import (
	"context"
	"fmt"

	"github.com/chiragsoni81245/p2p-storage/internal/core"
	"github.com/libp2p/go-libp2p/core/peer"
)

type PingHandler struct{}

func (h *PingHandler) Handle(ctx context.Context, peerID peer.ID, msg core.Message) (core.Message, error) {
	// Cast to your concrete type
	m, ok := msg.(Message)
	if !ok {
		return nil, fmt.Errorf("invalid message type")
	}

	fmt.Println("Received from", peerID, ":", m.Type)

	return Message{
		Type: "PONG",
		Data: "hello back",
	}, nil
}
