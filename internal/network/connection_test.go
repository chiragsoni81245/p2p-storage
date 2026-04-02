//go:build integration

package network

import (
	"context"
	"testing"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/discovery"
	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/chiragsoni81245/p2p-storage/internal/observability"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestHost(t *testing.T) (host.Host, func()) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	return h, func() { h.Close() }
}

func TestNewManager(t *testing.T) {
	h, cleanup := createTestHost(t)
	defer cleanup()

	bus := event.NewBus()
	logger := observability.NewLogger(observability.Fields{"service": "test"})

	manager := NewManager(logger, h, bus)

	require.NotNil(t, manager)
	assert.Equal(t, h, manager.host)
	assert.Equal(t, bus, manager.bus)
	assert.Equal(t, logger, manager.logger)
}

func TestManager_Connect_OnPeerDiscovered(t *testing.T) {
	h1, cleanup1 := createTestHost(t)
	defer cleanup1()

	h2, cleanup2 := createTestHost(t)
	defer cleanup2()

	bus := event.NewBus()
	logger := observability.NewLogger(observability.Fields{"service": "test"})

	// Create manager for h1
	_ = NewManager(logger, h1, bus)

	// Give the manager's listen goroutine time to start
	time.Sleep(50 * time.Millisecond)

	// Add h2's addresses to h1's peerstore
	h1.Peerstore().AddAddrs(h2.ID(), h2.Addrs(), time.Hour)

	// Publish peer discovered event
	bus.Publish(event.Event{
		Type: event.PeerDiscovered,
		Data: discovery.PeerDiscoveredEvent{
			AddrInfo: peer.AddrInfo{
				ID:    h2.ID(),
				Addrs: h2.Addrs(),
			},
		},
	})

	// Wait for connection
	require.Eventually(t, func() bool {
		return h1.Network().Connectedness(h2.ID()) == network.Connected
	}, 5*time.Second, 100*time.Millisecond, "h1 should connect to h2")
}

func TestManager_Connect_SkipsAlreadyConnected(t *testing.T) {
	h1, cleanup1 := createTestHost(t)
	defer cleanup1()

	h2, cleanup2 := createTestHost(t)
	defer cleanup2()

	bus := event.NewBus()
	logger := observability.NewLogger(observability.Fields{"service": "test"})

	// Pre-connect h1 to h2
	h1.Peerstore().AddAddrs(h2.ID(), h2.Addrs(), time.Hour)
	err := h1.Connect(context.Background(), peer.AddrInfo{ID: h2.ID(), Addrs: h2.Addrs()})
	require.NoError(t, err)

	// Create manager
	_ = NewManager(logger, h1, bus)

	// Publish peer discovered event (should be skipped since already connected)
	bus.Publish(event.Event{
		Type: event.PeerDiscovered,
		Data: discovery.PeerDiscoveredEvent{
			AddrInfo: peer.AddrInfo{
				ID:    h2.ID(),
				Addrs: h2.Addrs(),
			},
		},
	})

	// Connection state should remain Connected throughout
	require.Never(t, func() bool {
		return h1.Network().Connectedness(h2.ID()) != network.Connected
	}, 200*time.Millisecond, 50*time.Millisecond, "connection should remain stable")
}

func TestManager_Connect_HandlesBadAddress(t *testing.T) {
	h1, cleanup1 := createTestHost(t)
	defer cleanup1()

	bus := event.NewBus()
	logger := observability.NewLogger(observability.Fields{"service": "test"})

	// Create manager
	_ = NewManager(logger, h1, bus)

	// Publish discovery event with invalid peer (no addresses or unreachable)
	fakePeerID, _ := peer.Decode("QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N")

	bus.Publish(event.Event{
		Type: event.PeerDiscovered,
		Data: discovery.PeerDiscoveredEvent{
			AddrInfo: peer.AddrInfo{
				ID:    fakePeerID,
				Addrs: nil, // No addresses
			},
		},
	})

	// Should not connect — peer has no reachable addresses
	require.Never(t, func() bool {
		return h1.Network().Connectedness(fakePeerID) == network.Connected
	}, 200*time.Millisecond, 50*time.Millisecond, "should not connect to unreachable peer")
}
