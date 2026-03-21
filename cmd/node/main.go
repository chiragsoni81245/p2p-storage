package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/core"
	"github.com/chiragsoni81245/p2p-storage/internal/discovery"
	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/chiragsoni81245/p2p-storage/internal/middleware"
	"github.com/chiragsoni81245/p2p-storage/internal/network"
	"github.com/chiragsoni81245/p2p-storage/internal/node"
	"github.com/chiragsoni81245/p2p-storage/internal/protocol"
)

func main() {
	cfg := node.DefaultConfig()

	// Event bus to handle event pipelines
	bus := event.NewBus()

	n, err := node.NewNode(cfg)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Node started:", n.ID())
	for _, addr := range n.Addrs() {
		fmt.Printf("%s/p2p/%s\n", addr, n.ID())
	}

	// Attach network manager to control connections
	network.NewManager(cfg, n, bus)
	
	// Start discovery
	if err := discovery.StartMDNS(n, bus, "p2p-storage"); err != nil {
		log.Fatal(err)
	}

	// Initial handler which will control app working
	var handler core.Handler = &protocol.PingHandler{}

	// Apply Rate Limiting
	rl := middleware.NewRateLimiter(
		10,             // max 10 requests
		time.Second,    // per second
		5*time.Minute,  // cleanup inactive peers
	)
	handler = rl.Wrap(handler)

	protocolConfig := protocol.DefaultConfig()
	limiter := middleware.NewLimiter(100) // Max concurrent request 100
	scorer := network.NewPeerScorer() // Manage peer score and use best peers

	proto := protocol.New(n, "/app/1.0.0", handler, protocolConfig, limiter)

	go func() {
		for evt := range bus.Subscribe(event.PeerDiscovered) {
			pe := evt.Data.(discovery.PeerDiscoveredEvent)
			peerID := pe.AddrInfo.ID

			fmt.Printf("[%s] Peer discovered\n", peerID)

			scorer.RecordFailure(peerID)
		}
	}()

	go func() {
		for evt := range bus.Subscribe(event.PeerConnected) {
			pe := evt.Data.(network.PeerEvent)
			peerID := pe.PeerID

			fmt.Printf("[%s] Peer disconnected\n", peerID)

			scorer.RecordFailure(peerID)
		}
	}()

	go func() {
		for evt := range bus.Subscribe(event.PeerConnected) {
			pe := evt.Data.(network.PeerEvent)
			peerID := pe.PeerID

			fmt.Printf("[%s] Peer connected\n", peerID)

			start := time.Now()

			// This is the point peer touch once its connected successfully
			resp, err := proto.Send(context.Background(), peerID, protocol.Message{
				Type: "PING",
			})

			latency := time.Since(start)

			if err != nil {
				fmt.Println("send error:", err)
				scorer.RecordFailure(peerID)
				continue
			} else {
				scorer.RecordSuccess(peerID, latency)
			}

			fmt.Println("Response:", resp)
		}
	}()

	select {}
}
