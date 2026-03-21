package protocol

import (
	"context"
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"
)

type PingHandler struct{}

func (h *PingHandler) Handle(ctx context.Context, peerID peer.ID, msg Message) (Message, error) {
	fmt.Println("Received from", peerID, ":", msg.Type)

	return Message{
		Type: "PONG",
		Data: "hello back",
	}, nil
}
