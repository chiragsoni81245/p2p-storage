//go:build integration

package discovery

import (
	"fmt"
	"testing"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/chiragsoni81245/p2p-storage/internal/observability"
	"github.com/libp2p/go-libp2p"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultBootstrapConfig(t *testing.T) {
	cfg := DefaultBootstrapConfig()

	assert.Empty(t, cfg.BootstrapPeers)
	assert.Equal(t, 30*time.Second, cfg.ConnectionTimeout)
	assert.Equal(t, 10*time.Second, cfg.RetryInterval)
	assert.Equal(t, 3, cfg.MaxRetries)
}

func TestNewBootstrap(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := DefaultBootstrapConfig()
	logger := observability.NewLogger(observability.Fields{"service": "test"})

	bootstrap := NewBootstrap(h, bus, cfg, logger)

	assert.NotNil(t, bootstrap)
	assert.Equal(t, h, bootstrap.host)
	assert.Equal(t, bus, bootstrap.bus)
	assert.Equal(t, cfg, bootstrap.config)
}

func TestBootstrap_Start_NoPeers(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := DefaultBootstrapConfig()
	logger := observability.NewLogger(observability.Fields{"service": "test"})

	bootstrap := NewBootstrap(h, bus, cfg, logger)

	err = bootstrap.Start()
	assert.NoError(t, err)

	bootstrap.Stop()
}

func TestBootstrap_ConnectsToBootstrapPeer(t *testing.T) {
	h1, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h1.Close()

	h2, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h2.Close()

	bus := event.NewBus()
	ch := bus.Subscribe(event.PeerDiscovered)

	h1Addr := fmt.Sprintf("%s/p2p/%s", h1.Addrs()[0], h1.ID())

	cfg := BootstrapConfig{
		BootstrapPeers:    []string{h1Addr},
		ConnectionTimeout: 5 * time.Second,
		RetryInterval:     100 * time.Millisecond,
		MaxRetries:        1,
	}

	bootstrap := NewBootstrap(h2, bus, cfg, observability.NewLogger(observability.Fields{"service": "test"}))

	err = bootstrap.Start()
	require.NoError(t, err)

	select {
	case evt := <-ch:
		assert.Equal(t, event.PeerDiscovered, evt.Type)
		peerEvt, ok := evt.Data.(PeerDiscoveredEvent)
		require.True(t, ok)
		assert.Equal(t, h1.ID(), peerEvt.AddrInfo.ID)
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for peer discovered event")
	}

	bootstrap.Stop()
}

func TestBootstrap_IgnoresSelf(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	ch := bus.Subscribe(event.PeerDiscovered)

	selfAddr := fmt.Sprintf("%s/p2p/%s", h.Addrs()[0], h.ID())

	cfg := BootstrapConfig{
		BootstrapPeers:    []string{selfAddr},
		ConnectionTimeout: 1 * time.Second,
		RetryInterval:     100 * time.Millisecond,
		MaxRetries:        0,
	}

	bootstrap := NewBootstrap(h, bus, cfg, observability.NewLogger(observability.Fields{"service": "test"}))

	err = bootstrap.Start()
	require.NoError(t, err)

	select {
	case <-ch:
		t.Fatal("should not receive peer discovered event for self")
	case <-time.After(500 * time.Millisecond):
		// Expected
	}

	bootstrap.Stop()
}

func TestBootstrap_AddBootstrapPeer(t *testing.T) {
	h1, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h1.Close()

	h2, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h2.Close()

	bus := event.NewBus()
	ch := bus.Subscribe(event.PeerDiscovered)

	cfg := BootstrapConfig{
		BootstrapPeers:    []string{},
		ConnectionTimeout: 5 * time.Second,
		RetryInterval:     100 * time.Millisecond,
		MaxRetries:        1,
	}

	bootstrap := NewBootstrap(h2, bus, cfg, observability.NewLogger(observability.Fields{"service": "test"}))

	h1Addr := fmt.Sprintf("%s/p2p/%s", h1.Addrs()[0], h1.ID())
	err = bootstrap.AddBootstrapPeer(h1Addr)
	require.NoError(t, err)

	select {
	case evt := <-ch:
		assert.Equal(t, event.PeerDiscovered, evt.Type)
		peerEvt, ok := evt.Data.(PeerDiscoveredEvent)
		require.True(t, ok)
		assert.Equal(t, h1.ID(), peerEvt.AddrInfo.ID)
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for peer discovered event")
	}

	bootstrap.Stop()
}

func TestBootstrap_AddBootstrapPeer_InvalidAddress(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := DefaultBootstrapConfig()

	bootstrap := NewBootstrap(h, bus, cfg, observability.NewLogger(observability.Fields{"service": "test"}))

	err = bootstrap.AddBootstrapPeer("invalid-address")
	assert.Error(t, err)

	bootstrap.Stop()
}

func TestBootstrap_Stop(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := DefaultBootstrapConfig()

	bootstrap := NewBootstrap(h, bus, cfg, observability.NewLogger(observability.Fields{"service": "test"}))
	err = bootstrap.Start()
	require.NoError(t, err)

	bootstrap.Stop()
}
