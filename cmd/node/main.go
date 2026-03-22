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
	"github.com/chiragsoni81245/p2p-storage/internal/observability"
	"github.com/chiragsoni81245/p2p-storage/internal/protocol"
)

func main() {
	logger := observability.NewLogger(observability.Fields{})
	metrics := observability.NewMetrics()

	cfg := node.DefaultConfig()

	// Event bus to handle event pipelines
	bus := event.NewBus()

	n, err := node.NewNode(cfg)
	if err != nil {
		log.Fatal(err)
	}

	logger.Info("Node started", observability.Fields{"node_id": n.ID().String()})
	for _, addr := range n.Addrs() {
		logger.Info("peer address", observability.Fields{
			"address": fmt.Sprintf("%s/p2p/%s", addr, n.ID()),
		})
	}

	// Initial handler which will control app working
	var handler core.Handler = &protocol.PingHandler{
		Logger: logger,
	}

	// Apply Rate Limiting
	rl := middleware.NewRateLimiter(
		10,            // max 10 requests
		time.Second,   // per second
		5*time.Minute, // cleanup inactive peers
	)
	handler = rl.Wrap(handler)

	protocolConfig := protocol.DefaultConfig()
	limiter := middleware.NewLimiter(100) // Max concurrent request 100
	scorer := network.NewPeerScorer()     // Manage peer score and use best peers

	proto := protocol.New(n, "/app/1.0.0", handler, protocolConfig, logger, limiter)

	// Set up event subscriptions BEFORE starting discovery to avoid missing events
	go func() {
		for evt := range bus.Subscribe(event.PeerDiscovered) {
			pe := evt.Data.(discovery.PeerDiscoveredEvent)
			peerID := pe.AddrInfo.ID

			logger.Info("peer discovered", observability.Fields{
				"peer": peerID,
			})

			scorer.RecordFailure(peerID)
		}
	}()

	go func() {
		for evt := range bus.Subscribe(event.PeerDisconnected) {
			pe := evt.Data.(network.PeerEvent)
			peerID := pe.PeerID

			logger.Info("peer disconnected", observability.Fields{
				"peer":      peerID,
				"direction": pe.Conn.Stat().Direction,
			})

			scorer.RecordFailure(peerID)
		}
	}()

	go func() {
		for evt := range bus.Subscribe(event.PeerConnected) {
			pe := evt.Data.(network.PeerEvent)
			peerID := pe.PeerID

			logger.Info("peer connected", observability.Fields{
				"peer":      peerID,
				"direction": pe.Conn.Stat().Direction,
			})

			// This is the point peer touch once its connected successfully
			for range 5 {
				start := time.Now()
				resp, err := proto.Send(context.Background(), peerID, protocol.Message{
					Type: "PING",
				})
				latency := time.Since(start)

				if err != nil {
					logger.Error("send error", observability.Fields{
						"error": err,
					})
					metrics.RecordFailure()
					scorer.RecordFailure(peerID)
					continue
				} else {
					metrics.RecordRequest(latency)
					scorer.RecordSuccess(peerID, latency)
				}
				logger.Info("peer response", observability.Fields{"response": resp})

				time.Sleep(1 * time.Second)
			}

		}
	}()

	// Attach network manager to control connections
	// Must be after subscriptions to ensure events are received
	network.NewManager(cfg, logger, n, bus)

	// Start discovery
	if err := discovery.StartMDNS(n, bus, "p2p-storage"); err != nil {
		log.Fatal(err)
	}

	logger.Info("Discovery started", observability.Fields{})

	select {}
}
