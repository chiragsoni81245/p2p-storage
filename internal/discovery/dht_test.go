//go:build integration

package discovery

import (
	"testing"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultDHTConfig(t *testing.T) {
	cfg := DefaultDHTConfig()

	assert.Equal(t, "p2p-storage-network", cfg.RendezvousString)
	assert.Equal(t, 10*time.Second, cfg.DiscoveryInterval)
	assert.Equal(t, "auto", cfg.Mode)
	assert.NotEmpty(t, cfg.BootstrapPeers) // Should have IPFS bootstrap peers
}

func TestNewDHTDiscovery(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := DHTConfig{
		RendezvousString:  "test-network",
		DiscoveryInterval: 1 * time.Second,
		BootstrapPeers:    []peer.AddrInfo{},
		Mode:              "client",
	}

	dhtDiscovery := NewDHTDiscovery(h, bus, cfg)

	assert.NotNil(t, dhtDiscovery)
	assert.Equal(t, h, dhtDiscovery.host)
	assert.Equal(t, bus, dhtDiscovery.bus)
	assert.Equal(t, cfg.RendezvousString, dhtDiscovery.config.RendezvousString)
	assert.NotNil(t, dhtDiscovery.seenPeers)
}

func TestDHTDiscovery_Start_ClientMode(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := DHTConfig{
		RendezvousString:  "test-network",
		DiscoveryInterval: 1 * time.Second,
		BootstrapPeers:    []peer.AddrInfo{}, // No bootstrap peers for isolated test
		Mode:              "client",
	}

	dhtDiscovery := NewDHTDiscovery(h, bus, cfg)

	err = dhtDiscovery.Start()
	require.NoError(t, err)

	// DHT should be initialized
	assert.NotNil(t, dhtDiscovery.GetDHT())

	err = dhtDiscovery.Stop()
	assert.NoError(t, err)
}

func TestDHTDiscovery_Start_ServerMode(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := DHTConfig{
		RendezvousString:  "test-network",
		DiscoveryInterval: 1 * time.Second,
		BootstrapPeers:    []peer.AddrInfo{},
		Mode:              "server",
	}

	dhtDiscovery := NewDHTDiscovery(h, bus, cfg)

	err = dhtDiscovery.Start()
	require.NoError(t, err)

	assert.NotNil(t, dhtDiscovery.GetDHT())

	err = dhtDiscovery.Stop()
	assert.NoError(t, err)
}

func TestDHTDiscovery_Start_AutoMode(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := DHTConfig{
		RendezvousString:  "test-network",
		DiscoveryInterval: 1 * time.Second,
		BootstrapPeers:    []peer.AddrInfo{},
		Mode:              "auto",
	}

	dhtDiscovery := NewDHTDiscovery(h, bus, cfg)

	err = dhtDiscovery.Start()
	require.NoError(t, err)

	assert.NotNil(t, dhtDiscovery.GetDHT())

	err = dhtDiscovery.Stop()
	assert.NoError(t, err)
}

func TestDHTDiscovery_StartTwice(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := DHTConfig{
		RendezvousString:  "test-network",
		DiscoveryInterval: 1 * time.Second,
		BootstrapPeers:    []peer.AddrInfo{},
		Mode:              "client",
	}

	dhtDiscovery := NewDHTDiscovery(h, bus, cfg)

	err = dhtDiscovery.Start()
	require.NoError(t, err)

	// Starting twice should be a no-op
	err = dhtDiscovery.Start()
	assert.NoError(t, err)

	err = dhtDiscovery.Stop()
	assert.NoError(t, err)
}

func TestDHTDiscovery_PeerDiscovery(t *testing.T) {
	// Create two hosts that will discover each other via DHT
	h1, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h1.Close()

	h2, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h2.Close()

	bus1 := event.NewBus()
	bus2 := event.NewBus()

	// Use custom DHT options for isolated testing
	rendezvous := "test-discovery-network"

	cfg1 := DHTConfig{
		RendezvousString:  rendezvous,
		DiscoveryInterval: 500 * time.Millisecond,
		BootstrapPeers:    []peer.AddrInfo{},
		Mode:              "server",
	}

	cfg2 := DHTConfig{
		RendezvousString:  rendezvous,
		DiscoveryInterval: 500 * time.Millisecond,
		BootstrapPeers: []peer.AddrInfo{{
			ID:    h1.ID(),
			Addrs: h1.Addrs(),
		}},
		Mode: "server",
	}

	dht1 := NewDHTDiscovery(h1, bus1, cfg1)
	dht2 := NewDHTDiscovery(h2, bus2, cfg2)

	ch := bus2.Subscribe(event.PeerDiscovered)

	err = dht1.Start()
	require.NoError(t, err)

	err = dht2.Start()
	require.NoError(t, err)

	// Wait for peer discovery
	select {
	case evt := <-ch:
		assert.Equal(t, event.PeerDiscovered, evt.Type)
		peerEvt, ok := evt.Data.(PeerDiscoveredEvent)
		require.True(t, ok)
		assert.Equal(t, h1.ID(), peerEvt.AddrInfo.ID)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for peer discovery")
	}

	dht1.Stop()
	dht2.Stop()
}

func TestDHTDiscovery_GetDHT_NilBeforeStart(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := DHTConfig{
		RendezvousString:  "test-network",
		DiscoveryInterval: 1 * time.Second,
		BootstrapPeers:    []peer.AddrInfo{},
		Mode:              "client",
	}

	dhtDiscovery := NewDHTDiscovery(h, bus, cfg)

	// DHT should be nil before Start
	assert.Nil(t, dhtDiscovery.GetDHT())
}

func TestDHTDiscovery_FindPeer(t *testing.T) {
	// Create two connected hosts
	h1, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h1.Close()

	h2, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h2.Close()

	bus1 := event.NewBus()
	bus2 := event.NewBus()

	cfg1 := DHTConfig{
		RendezvousString:  "find-peer-test",
		DiscoveryInterval: 500 * time.Millisecond,
		BootstrapPeers:    []peer.AddrInfo{},
		Mode:              "server",
	}

	cfg2 := DHTConfig{
		RendezvousString:  "find-peer-test",
		DiscoveryInterval: 500 * time.Millisecond,
		BootstrapPeers: []peer.AddrInfo{{
			ID:    h1.ID(),
			Addrs: h1.Addrs(),
		}},
		Mode: "server",
	}

	dht1 := NewDHTDiscovery(h1, bus1, cfg1)
	dht2 := NewDHTDiscovery(h2, bus2, cfg2)

	err = dht1.Start()
	require.NoError(t, err)

	err = dht2.Start()
	require.NoError(t, err)

	// Wait for connection
	time.Sleep(1 * time.Second)

	// Try to find h1 from h2's perspective
	peerInfo, err := dht2.FindPeer(h1.ID())
	require.NoError(t, err)
	assert.Equal(t, h1.ID(), peerInfo.ID)

	dht1.Stop()
	dht2.Stop()
}

func TestDHTDiscovery_Stop_NilDHT(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	bus := event.NewBus()
	cfg := DHTConfig{
		RendezvousString:  "test-network",
		DiscoveryInterval: 1 * time.Second,
		BootstrapPeers:    []peer.AddrInfo{},
		Mode:              "client",
	}

	dhtDiscovery := NewDHTDiscovery(h, bus, cfg)

	// Stop without starting should not error
	err = dhtDiscovery.Stop()
	assert.NoError(t, err)
}

func TestDHTConfig_GetDefaultBootstrapPeers(t *testing.T) {
	peers := dht.GetDefaultBootstrapPeerAddrInfos()
	assert.NotEmpty(t, peers, "Should have default IPFS bootstrap peers")
}
