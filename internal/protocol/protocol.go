package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/core"
	"github.com/chiragsoni81245/p2p-storage/internal/middleware"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

type Protocol struct {
	host    host.Host
	cfg     Config
	handler core.Handler
	id      protocol.ID
	limiter *middleware.Limiter
}

func New(host host.Host, protocolID protocol.ID, handler core.Handler, cfg Config, limiter *middleware.Limiter) *Protocol {
	p := &Protocol{
		host:    host,
		cfg:     cfg,
		handler: handler,
		id:      protocolID,
		limiter: limiter,
	}

	host.SetStreamHandler(protocolID, p.handleStream)

	return p
}

func (p *Protocol) handleStream(s network.Stream) {
	/*
		Here we try to acquire a slot to make sure we can spin a gorutine
		This make sure we do not spin too much gorutines at same time
	*/
	if !p.limiter.Acquire() {
		fmt.Println("Dropping request: overloaded")
		_ = s.Reset() // aggressively close
		return
	}
	defer p.limiter.Release()


	defer s.Close()

	// Set read deadline
	_ = s.SetReadDeadline(time.Now().Add(p.cfg.ReadTimeout))

	// Limit input size
	limitedReader := io.LimitReader(s, p.cfg.MaxMessageSize)

	decoder := json.NewDecoder(limitedReader)

	var msg Message
	if err := decoder.Decode(&msg); err != nil {
		fmt.Println("decode error:", err)
		return
	}

	// Reset read deadline (optional)
	_ = s.SetReadDeadline(time.Time{})

	// Handle with timeout
	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.HandlerTimeout)
	defer cancel()

	resp, err := p.handler.Handle(
		ctx,
		s.Conn().RemotePeer(),
		msg,
	)
	if err != nil {
		fmt.Println("handler error:", err)
		return
	}

	// Set write deadline
	_ = s.SetWriteDeadline(time.Now().Add(p.cfg.WriteTimeout))

	encoder := json.NewEncoder(s)
	if err := encoder.Encode(resp); err != nil {
		fmt.Println("encode error:", err)
		return
	}
}

func (p *Protocol) Send(ctx context.Context, peerID peer.ID, msg Message) (Message, error) {
	ctx, cancel := context.WithTimeout(ctx, p.cfg.HandlerTimeout)
	defer cancel()

	stream, err := p.host.NewStream(ctx, peerID, p.id)
	if err != nil {
		return Message{}, err
	}
	defer stream.Close()

	// write deadline
	_ = stream.SetWriteDeadline(time.Now().Add(p.cfg.WriteTimeout))

	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(msg); err != nil {
		return Message{}, err
	}

	// read deadline
	_ = stream.SetReadDeadline(time.Now().Add(p.cfg.ReadTimeout))

	limitedReader := io.LimitReader(stream, p.cfg.MaxMessageSize)
	decoder := json.NewDecoder(limitedReader)

	var resp Message
	if err := decoder.Decode(&resp); err != nil {
		return Message{}, err
	}

	return resp, nil
}
