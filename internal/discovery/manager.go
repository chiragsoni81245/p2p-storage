package discovery

import (
	"sync"

	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/chiragsoni81245/p2p-storage/internal/observability"
	"github.com/libp2p/go-libp2p/core/host"
)

// DiscoveryMethod represents different discovery methods
type DiscoveryMethod string

const (
	MethodMDNS      DiscoveryMethod = "mdns"
	MethodDHT       DiscoveryMethod = "dht"
	MethodBootstrap DiscoveryMethod = "bootstrap"
)

// Config holds the configuration for all discovery methods
type Config struct {
	// EnabledMethods specifies which discovery methods to use
	// If empty, defaults to mDNS only (for backwards compatibility)
	EnabledMethods []DiscoveryMethod

	// MDNS configuration
	MDNS MDNSConfig

	// DHT configuration
	DHT DHTConfig

	// Bootstrap configuration
	Bootstrap BootstrapConfig
}

// DefaultConfig returns the default discovery configuration
func DefaultConfig() Config {
	return Config{
		EnabledMethods: []DiscoveryMethod{MethodMDNS},
		MDNS:           DefaultMDNSConfig(),
		DHT:            DefaultDHTConfig(),
		Bootstrap:      DefaultBootstrapConfig(),
	}
}

// Manager manages multiple discovery services
type Manager struct {
	host   host.Host
	bus    *event.Bus
	config Config
	logger *observability.Logger

	mu           sync.RWMutex
	bootstrap    *Bootstrap
	dhtDiscovery *DHTDiscovery
	mdnsStarted  bool
}

// NewManager creates a new discovery manager
func NewManager(h host.Host, bus *event.Bus, cfg Config, logger *observability.Logger) *Manager {
	// Default to mDNS only if no methods specified
	if len(cfg.EnabledMethods) == 0 {
		cfg.EnabledMethods = []DiscoveryMethod{MethodMDNS}
	}

	return &Manager{
		host:   h,
		bus:    bus,
		config: cfg,
		logger: logger,
	}
}

// Start starts all configured discovery methods
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, method := range m.config.EnabledMethods {
		switch method {
		case MethodMDNS:
			if err := m.startMDNS(); err != nil {
				return err
			}
		case MethodDHT:
			if err := m.startDHT(); err != nil {
				return err
			}
		case MethodBootstrap:
			if err := m.startBootstrap(); err != nil {
				return err
			}
		}
	}

	return nil
}

// Stop stops all discovery services
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var lastErr error

	if m.bootstrap != nil {
		m.bootstrap.Stop()
	}

	if m.dhtDiscovery != nil {
		if err := m.dhtDiscovery.Stop(); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

func (m *Manager) startMDNS() error {
	if m.mdnsStarted {
		return nil
	}

	if err := StartMDNSWithConfig(m.host, m.bus, m.config.MDNS, m.logger); err != nil {
		return err
	}

	m.mdnsStarted = true
	return nil
}

func (m *Manager) startDHT() error {
	if m.dhtDiscovery != nil {
		return nil
	}

	m.dhtDiscovery = NewDHTDiscovery(m.host, m.bus, m.config.DHT)
	return m.dhtDiscovery.Start()
}

func (m *Manager) startBootstrap() error {
	if m.bootstrap != nil {
		return nil
	}

	m.bootstrap = NewBootstrap(m.host, m.bus, m.config.Bootstrap, m.logger)
	return m.bootstrap.Start()
}

// GetDHT returns the underlying DHT (nil if DHT discovery not enabled)
func (m *Manager) GetDHT() *DHTDiscovery {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.dhtDiscovery
}

// GetBootstrap returns the bootstrap service (nil if not enabled)
func (m *Manager) GetBootstrap() *Bootstrap {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.bootstrap
}

// AddBootstrapPeer dynamically adds a bootstrap peer
func (m *Manager) AddBootstrapPeer(addr string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.bootstrap != nil {
		return m.bootstrap.AddBootstrapPeer(addr)
	}
	return nil
}

// IsMethodEnabled checks if a discovery method is enabled
func (m *Manager) IsMethodEnabled(method DiscoveryMethod) bool {
	for _, enabled := range m.config.EnabledMethods {
		if enabled == method {
			return true
		}
	}
	return false
}
