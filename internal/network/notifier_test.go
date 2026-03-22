//go:build integration

package network

import (
	"context"
	"testing"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewNotifier(t *testing.T) {
	bus := event.NewBus()
	notifier := NewNotifier(bus)

	require.NotNil(t, notifier)
	assert.Equal(t, bus, notifier.bus)
}

func TestNotifier_ImplementsNetworkNotifier(t *testing.T) {
	bus := event.NewBus()
	notifier := NewNotifier(bus)

	var _ network.Notifiee = notifier
}

func TestNotifier_Connected(t *testing.T) {
	bus := event.NewBus()
	notifier := NewNotifier(bus)

	// Subscribe to connected events
	ch := bus.Subscribe(event.PeerConnected)

	// Create two real libp2p hosts for testing
	h1, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h1.Close()

	h2, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h2.Close()

	// Register notifier on h1
	h1.Network().Notify(notifier)

	// Connect h1 to h2
	h1.Peerstore().AddAddrs(h2.ID(), h2.Addrs(), time.Hour)
	err = h1.Connect(context.Background(), peer.AddrInfo{ID: h2.ID(), Addrs: h2.Addrs()})
	require.NoError(t, err)

	// Wait for connected event
	select {
	case evt := <-ch:
		assert.Equal(t, event.PeerConnected, evt.Type)
		peerEvt, ok := evt.Data.(PeerEvent)
		require.True(t, ok)
		assert.Equal(t, h2.ID(), peerEvt.PeerID)
		assert.NotNil(t, peerEvt.Conn)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for PeerConnected event")
	}
}

func TestNotifier_Disconnected(t *testing.T) {
	bus := event.NewBus()
	notifier := NewNotifier(bus)

	// Subscribe to disconnected events
	chDisconnected := bus.Subscribe(event.PeerDisconnected)
	chConnected := bus.Subscribe(event.PeerConnected)

	// Create two real libp2p hosts
	h1, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h1.Close()

	h2, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h2.Close()

	// Register notifier on h1
	h1.Network().Notify(notifier)

	// Connect h1 to h2
	h1.Peerstore().AddAddrs(h2.ID(), h2.Addrs(), time.Hour)
	err = h1.Connect(context.Background(), peer.AddrInfo{ID: h2.ID(), Addrs: h2.Addrs()})
	require.NoError(t, err)

	// Wait for connected event first
	select {
	case <-chConnected:
		// Good, connected
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for connection")
	}

	// Close h2 to trigger disconnect
	h2.Close()

	// Wait for disconnected event
	select {
	case evt := <-chDisconnected:
		assert.Equal(t, event.PeerDisconnected, evt.Type)
		peerEvt, ok := evt.Data.(PeerEvent)
		require.True(t, ok)
		assert.Equal(t, h2.ID(), peerEvt.PeerID)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for PeerDisconnected event")
	}
}

func TestNotifier_Listen_NoOp(t *testing.T) {
	bus := event.NewBus()
	notifier := NewNotifier(bus)

	// Should not panic
	assert.NotPanics(t, func() {
		notifier.Listen(nil, nil)
	})
}

func TestNotifier_ListenClose_NoOp(t *testing.T) {
	bus := event.NewBus()
	notifier := NewNotifier(bus)

	// Should not panic
	assert.NotPanics(t, func() {
		notifier.ListenClose(nil, nil)
	})
}

func TestPeerEvent_Struct(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	evt := PeerEvent{
		PeerID: h.ID(),
		Conn:   nil, // Connection can be nil for structural test
	}

	assert.Equal(t, h.ID(), evt.PeerID)
}
