//go:build integration

package discovery

import (
	"fmt"
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

// testLogger returns a silent logger for tests
func testLogger() *observability.Logger {
	return observability.NewLoggerWithWriter(io.Discard, observability.Fields{})
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, []DiscoveryMethod{MethodMDNS}, cfg.EnabledMethods)
	assert.NotEmpty(t, cfg.MDNS.ServiceName)
	assert.NotEmpty(t, cfg.DHT.RendezvousString)
	assert.Equal(t, 30*time.Second, cfg.Bootstrap.ConnectionTimeout)
}

func TestDiscoveryMethod_Constants(t *testing.T) {
	assert.Equal(t, DiscoveryMethod("mdns"), MethodMDNS)
	assert.Equal(t, DiscoveryMethod("dht"), MethodDHT)
	assert.Equal(t, DiscoveryMethod("bootstrap"), MethodBootstrap)
}

func TestNewManager(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := DefaultConfig()

	mgr := NewManager(h, bus, cfg, testLogger())

	assert.NotNil(t, mgr)
	assert.Equal(t, h, mgr.host)
	assert.Equal(t, bus, mgr.bus)
}

func TestNewManager_DefaultsMDNS(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := Config{
		EnabledMethods: []DiscoveryMethod{}, // Empty
		MDNS:           DefaultMDNSConfig(),
	}

	mgr := NewManager(h, bus, cfg, testLogger())

	// Should default to mDNS
	assert.Equal(t, []DiscoveryMethod{MethodMDNS}, mgr.config.EnabledMethods)
}

func TestManager_Start_MDNSOnly(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := Config{
		EnabledMethods: []DiscoveryMethod{MethodMDNS},
		MDNS:           DefaultMDNSConfig(),
	}

	mgr := NewManager(h, bus, cfg, testLogger())

	err = mgr.Start()
	assert.NoError(t, err)

	assert.True(t, mgr.mdnsStarted)
	assert.Nil(t, mgr.GetDHT())
	assert.Nil(t, mgr.GetBootstrap())

	mgr.Stop()
}

func TestManager_Start_DHTOnly(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := Config{
		EnabledMethods: []DiscoveryMethod{MethodDHT},
		DHT: DHTConfig{
			RendezvousString:  "test-network",
			DiscoveryInterval: 1 * time.Second,
			BootstrapPeers:    []peer.AddrInfo{},
			Mode:              "client",
		},
	}

	mgr := NewManager(h, bus, cfg, testLogger())

	err = mgr.Start()
	assert.NoError(t, err)

	assert.False(t, mgr.mdnsStarted)
	assert.NotNil(t, mgr.GetDHT())
	assert.Nil(t, mgr.GetBootstrap())

	mgr.Stop()
}

func TestManager_Start_BootstrapOnly(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := Config{
		EnabledMethods: []DiscoveryMethod{MethodBootstrap},
		Bootstrap:      DefaultBootstrapConfig(),
	}

	mgr := NewManager(h, bus, cfg, testLogger())

	err = mgr.Start()
	assert.NoError(t, err)

	assert.False(t, mgr.mdnsStarted)
	assert.Nil(t, mgr.GetDHT())
	assert.NotNil(t, mgr.GetBootstrap())

	mgr.Stop()
}

func TestManager_Start_AllMethods(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := Config{
		EnabledMethods: []DiscoveryMethod{MethodMDNS, MethodDHT, MethodBootstrap},
		MDNS:           DefaultMDNSConfig(),
		DHT: DHTConfig{
			RendezvousString:  "test-network",
			DiscoveryInterval: 1 * time.Second,
			BootstrapPeers:    []peer.AddrInfo{},
			Mode:              "client",
		},
		Bootstrap: DefaultBootstrapConfig(),
	}

	mgr := NewManager(h, bus, cfg, testLogger())

	err = mgr.Start()
	assert.NoError(t, err)

	assert.True(t, mgr.mdnsStarted)
	assert.NotNil(t, mgr.GetDHT())
	assert.NotNil(t, mgr.GetBootstrap())

	mgr.Stop()
}

func TestManager_IsMethodEnabled(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := Config{
		EnabledMethods: []DiscoveryMethod{MethodMDNS, MethodDHT},
		MDNS:           DefaultMDNSConfig(),
		DHT:            DefaultDHTConfig(),
	}

	mgr := NewManager(h, bus, cfg, testLogger())

	assert.True(t, mgr.IsMethodEnabled(MethodMDNS))
	assert.True(t, mgr.IsMethodEnabled(MethodDHT))
	assert.False(t, mgr.IsMethodEnabled(MethodBootstrap))
}

func TestManager_AddBootstrapPeer(t *testing.T) {
	// Create bootstrap peer
	h1, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h1.Close()

	// Create main host
	h2, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h2.Close()

	bus := event.NewBus()
	ch := bus.Subscribe(event.PeerDiscovered)

	cfg := Config{
		EnabledMethods: []DiscoveryMethod{MethodBootstrap},
		Bootstrap: BootstrapConfig{
			BootstrapPeers:    []string{},
			ConnectionTimeout: 5 * time.Second,
			RetryInterval:     100 * time.Millisecond,
			MaxRetries:        1,
		},
	}

	mgr := NewManager(h2, bus, cfg, testLogger())

	err = mgr.Start()
	require.NoError(t, err)

	// Add bootstrap peer dynamically
	h1Addr := fmt.Sprintf("%s/p2p/%s", h1.Addrs()[0], h1.ID())
	err = mgr.AddBootstrapPeer(h1Addr)
	assert.NoError(t, err)

	// Should receive peer discovered event
	select {
	case evt := <-ch:
		assert.Equal(t, event.PeerDiscovered, evt.Type)
		peerEvt, ok := evt.Data.(PeerDiscoveredEvent)
		require.True(t, ok)
		assert.Equal(t, h1.ID(), peerEvt.AddrInfo.ID)
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for peer discovered event")
	}

	mgr.Stop()
}

func TestManager_AddBootstrapPeer_NoBootstrapService(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := Config{
		EnabledMethods: []DiscoveryMethod{MethodMDNS}, // No bootstrap
		MDNS:           DefaultMDNSConfig(),
	}

	mgr := NewManager(h, bus, cfg, testLogger())

	err = mgr.Start()
	require.NoError(t, err)

	// Should not error even though bootstrap is not enabled
	err = mgr.AddBootstrapPeer("/ip4/127.0.0.1/tcp/1234/p2p/QmTest123")
	assert.NoError(t, err)

	mgr.Stop()
}

func TestManager_Stop(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := Config{
		EnabledMethods: []DiscoveryMethod{MethodDHT, MethodBootstrap},
		DHT: DHTConfig{
			RendezvousString:  "test-network",
			DiscoveryInterval: 1 * time.Second,
			BootstrapPeers:    []peer.AddrInfo{},
			Mode:              "client",
		},
		Bootstrap: DefaultBootstrapConfig(),
	}

	mgr := NewManager(h, bus, cfg, testLogger())

	err = mgr.Start()
	require.NoError(t, err)

	// Stop should clean up all services
	err = mgr.Stop()
	assert.NoError(t, err)
}

func TestManager_Stop_BeforeStart(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := DefaultConfig()

	mgr := NewManager(h, bus, cfg, testLogger())

	// Stop before Start should not error
	err = mgr.Stop()
	assert.NoError(t, err)
}

func TestManager_StartMDNSTwice(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := Config{
		EnabledMethods: []DiscoveryMethod{MethodMDNS},
		MDNS:           DefaultMDNSConfig(),
	}

	mgr := NewManager(h, bus, cfg, testLogger())

	err = mgr.Start()
	require.NoError(t, err)

	// Starting again should be a no-op
	err = mgr.Start()
	assert.NoError(t, err)

	mgr.Stop()
}

func TestManager_GetDHT_Nil(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := Config{
		EnabledMethods: []DiscoveryMethod{MethodMDNS},
		MDNS:           DefaultMDNSConfig(),
	}

	mgr := NewManager(h, bus, cfg, testLogger())

	// DHT not enabled
	assert.Nil(t, mgr.GetDHT())
}

func TestManager_GetBootstrap_Nil(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := Config{
		EnabledMethods: []DiscoveryMethod{MethodMDNS},
		MDNS:           DefaultMDNSConfig(),
	}

	mgr := NewManager(h, bus, cfg, testLogger())

	// Bootstrap not enabled
	assert.Nil(t, mgr.GetBootstrap())
}
