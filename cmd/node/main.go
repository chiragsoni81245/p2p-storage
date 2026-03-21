package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/discovery"
	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/chiragsoni81245/p2p-storage/internal/middleware"
	"github.com/chiragsoni81245/p2p-storage/internal/network"
	"github.com/chiragsoni81245/p2p-storage/internal/node"
	"github.com/chiragsoni81245/p2p-storage/internal/protocol"
	"github.com/libp2p/go-libp2p/core/peer"
)

func main() {
	cfg := node.DefaultConfig()

	n, err := node.NewNode(cfg)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Node started:", n.ID())

	for _, addr := range n.Addrs() {
		fmt.Printf("%s/p2p/%s\n", addr, n.ID())
	}

	bus := event.NewBus()

	network.NewManager(cfg, n, bus)
	
	// Start discovery
	if err := discovery.StartMDNS(n, bus, "p2p-storage"); err != nil {
		log.Fatal(err)
	}

	var handler protocol.Handler
	handler = &protocol.PingHandler{}

	// Apply Rate Limiting
	rl := middleware.NewRateLimiter(
		10,             // max 10 requests
		time.Second,    // per second
		5*time.Minute,  // cleanup inactive peers
	)
	handler = rl.Wrap(handler)

	protocolConfig := protocol.DefaultConfig()
	proto := protocol.New(n, protocolConfig, "/app/1.0.0", handler)

	go func() {
		ch := bus.Subscribe(event.PeerConnected)

		for evt := range ch {
			pi := evt.Data.(peer.AddrInfo)

			// This is the point peer touch once its connected successfully
			resp, err := proto.Send(context.Background(), pi.ID, protocol.Message{
				Type: "PING",
			})
			if err != nil {
				fmt.Println("send error:", err)
				continue
			}

			fmt.Println("Response:", resp)
		}
	}()

	select {}
}
