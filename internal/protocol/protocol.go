package protocol

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

type Protocol struct {
	host    host.Host
	handler Handler
	id      protocol.ID
}

func New(host host.Host, protocolID protocol.ID, handler Handler) *Protocol {
	p := &Protocol{
		host:    host,
		handler: handler,
		id:      protocolID,
	}

	host.SetStreamHandler(protocolID, p.handleStream)

	return p
}

func (p *Protocol) handleStream(s network.Stream) {
	defer s.Close()

	decoder := json.NewDecoder(s)
	encoder := json.NewEncoder(s)

	var msg Message
	if err := decoder.Decode(&msg); err != nil {
		fmt.Println("decode error:", err)
		return
	}

	resp, err := p.handler.Handle(
		context.Background(),
		s.Conn().RemotePeer(),
		msg,
	)
	if err != nil {
		fmt.Println("handler error:", err)
		return
	}

	if err := encoder.Encode(resp); err != nil {
		fmt.Println("encode error:", err)
	}
}

func (p *Protocol) Send(ctx context.Context, peerID peer.ID, msg Message) (Message, error) {
	stream, err := p.host.NewStream(ctx, peerID, p.id)
	if err != nil {
		return Message{}, err
	}
	defer stream.Close()

	encoder := json.NewEncoder(stream)
	decoder := json.NewDecoder(stream)

	if err := encoder.Encode(msg); err != nil {
		return Message{}, err
	}

	var resp Message
	if err := decoder.Decode(&resp); err != nil {
		return Message{}, err
	}

	return resp, nil
}
