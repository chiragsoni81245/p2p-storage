package main

import (
	"context"
	"fmt"
	"log"

	"github.com/chiragsoni81245/p2p-storage/internal/discovery"
	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/chiragsoni81245/p2p-storage/internal/network"
	"github.com/chiragsoni81245/p2p-storage/internal/node"
	"github.com/chiragsoni81245/p2p-storage/internal/protocol"
	"github.com/libp2p/go-libp2p/core/peer"
)

func main() {
	cfg := node.Config{
		ListenPort: 0,
		MinConnection: 50,
		MaxConnection: 100,
	}

	n, err := node.NewNode(cfg)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Node started:", n.ID())

	for _, addr := range n.Addrs() {
		fmt.Printf("%s/p2p/%s\n", addr, n.ID())
	}

	bus := event.NewBus()

	network.NewManager(n, bus, cfg.MaxConnection)
	
	// Start discovery
	if err := discovery.StartMDNS(n, bus, "p2p-storage"); err != nil {
		log.Fatal(err)
	}

	proto := protocol.New(n, "/app/1.0.0", &protocol.PingHandler{})

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
