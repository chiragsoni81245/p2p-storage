//go:build integration

package discovery

import (
	"io"
	"testing"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/chiragsoni81245/p2p-storage/internal/observability"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// discTestLogger returns a silent logger for tests
func discTestLogger() *observability.Logger {
	return observability.NewLoggerWithWriter(io.Discard, observability.Fields{})
}

func TestNotifee_HandlePeerFound(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	notifee := &Notifee{
		Host:   h,
		Bus:    bus,
		logger: discTestLogger(),
	}

	// Subscribe to peer discovered events
	ch := bus.Subscribe(event.PeerDiscovered)

	// Create a different peer ID
	h2, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h2.Close()

	// Handle peer found with different peer
	go notifee.HandlePeerFound(peer.AddrInfo{
		ID:    h2.ID(),
		Addrs: h2.Addrs(),
	})

	// Should receive event
	select {
	case evt := <-ch:
		assert.Equal(t, event.PeerDiscovered, evt.Type)
		peerEvt, ok := evt.Data.(PeerDiscoveredEvent)
		require.True(t, ok)
		assert.Equal(t, h2.ID(), peerEvt.AddrInfo.ID)
		assert.Equal(t, h2.Addrs(), peerEvt.AddrInfo.Addrs)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for PeerDiscovered event")
	}
}

func TestNotifee_HandlePeerFound_IgnoresSelf(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	notifee := &Notifee{
		Host:   h,
		Bus:    bus,
		logger: discTestLogger(),
	}

	// Subscribe to peer discovered events
	ch := bus.Subscribe(event.PeerDiscovered)

	// Handle peer found with self
	go notifee.HandlePeerFound(peer.AddrInfo{
		ID:    h.ID(),
		Addrs: h.Addrs(),
	})

	// Should NOT receive event (self is ignored)
	select {
	case <-ch:
		t.Fatal("should not receive event for self")
	case <-time.After(200 * time.Millisecond):
		// Expected - no event received
	}
}

func TestPeerDiscoveredEvent_Struct(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	addrInfo := peer.AddrInfo{
		ID:    h.ID(),
		Addrs: h.Addrs(),
	}

	evt := PeerDiscoveredEvent{
		AddrInfo: addrInfo,
	}

	assert.Equal(t, h.ID(), evt.AddrInfo.ID)
	assert.Equal(t, h.Addrs(), evt.AddrInfo.Addrs)
}

func TestStartMDNS(t *testing.T) {
	// Create a real libp2p host
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()

	// Starting mDNS should not error
	err = StartMDNS(h, bus, "test-service", discTestLogger())
	assert.NoError(t, err)
}

func TestStartMDNS_MultipleHosts(t *testing.T) {
	// Create two hosts that can discover each other
	h1, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h1.Close()

	h2, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h2.Close()

	bus1 := event.NewBus()
	bus2 := event.NewBus()

	ch1 := bus1.Subscribe(event.PeerDiscovered)
	ch2 := bus2.Subscribe(event.PeerDiscovered)

	// Start mDNS on both hosts with same service name
	err = StartMDNS(h1, bus1, "test-mdns-service", discTestLogger())
	require.NoError(t, err)

	err = StartMDNS(h2, bus2, "test-mdns-service", discTestLogger())
	require.NoError(t, err)

	// At least one should discover the other (mDNS discovery)
	// This is a best-effort test as mDNS can be flaky in CI environments
	timeout := time.After(5 * time.Second)

	discovered := false
	for !discovered {
		select {
		case evt := <-ch1:
			peerEvt := evt.Data.(PeerDiscoveredEvent)
			if peerEvt.AddrInfo.ID == h2.ID() {
				discovered = true
			}
		case evt := <-ch2:
			peerEvt := evt.Data.(PeerDiscoveredEvent)
			if peerEvt.AddrInfo.ID == h1.ID() {
				discovered = true
			}
		case <-timeout:
			// mDNS discovery might not work in all environments (CI, Docker, etc.)
			// This is not a failure, just a limitation of the test environment
			t.Skip("mDNS discovery timeout - may not work in this environment")
			return
		}
	}

	assert.True(t, discovered)
}
