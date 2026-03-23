package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/discovery"
	"github.com/chiragsoni81245/p2p-storage/internal/middleware"
	"github.com/chiragsoni81245/p2p-storage/internal/node"
	"github.com/chiragsoni81245/p2p-storage/internal/protocol"
	"github.com/chiragsoni81245/p2p-storage/internal/store"
	"gopkg.in/yaml.v3"
)

// DefaultConfigPath is the default path for config file
const DefaultConfigPath = "./config.yaml"

type Config struct {
	NodeConfig        node.Config
	RateLimiterConfig middleware.RateLimiterConfig
	ProtocolConfig    protocol.Config
	Encryption        store.EncryptionConfig
}

// CLIOverrides holds CLI flag values that override YAML config
type CLIOverrides struct {
	ConfigPath     string
	StorageRoot    string
	PeerWait       time.Duration
	Timeout        time.Duration
	LogFile        string
	DiscoveryModes string
	BootstrapPeers []string
}

// YAMLConfig represents the YAML configuration file structure
type YAMLConfig struct {
	StorageRoot string        `yaml:"storage_root"`
	LogFile     string        `yaml:"log_file"`
	PeerWait    time.Duration `yaml:"peer_wait"`
	Timeout     time.Duration `yaml:"timeout"`

	Node        YAMLNodeConfig        `yaml:"node"`
	Protocol    YAMLProtocolConfig    `yaml:"protocol"`
	RateLimiter YAMLRateLimiterConfig `yaml:"rate_limiter"`
	Encryption  YAMLEncryptionConfig  `yaml:"encryption"`
}

// YAMLNodeConfig represents node configuration in YAML
type YAMLNodeConfig struct {
	ListenPort    int    `yaml:"listen_port"`
	IdentityPath  string `yaml:"identity_path"`
	MinConnection int    `yaml:"min_connection"`
	MaxConnection int    `yaml:"max_connection"`
	Concurrency   int    `yaml:"concurrency"`

	// NAT Traversal
	EnableRelay        bool     `yaml:"enable_relay"`
	EnableHolePunch    bool     `yaml:"enable_hole_punch"`
	EnableAutoNAT      bool     `yaml:"enable_auto_nat"`
	RelayServers       []string `yaml:"relay_servers"`
	EnableRelayService bool     `yaml:"enable_relay_service"`

	// Address announcement (for relay servers on EC2/cloud)
	ExternalIP string `yaml:"external_ip"`

	Discovery YAMLDiscoveryConfig `yaml:"discovery"`
}

// YAMLDiscoveryConfig represents discovery configuration in YAML
type YAMLDiscoveryConfig struct {
	EnabledMethods []string `yaml:"enabled_methods"`

	MDNS      YAMLMDNSConfig      `yaml:"mdns"`
	DHT       YAMLDHTConfig       `yaml:"dht"`
	Bootstrap YAMLBootstrapConfig `yaml:"bootstrap"`
}

// YAMLMDNSConfig represents mDNS configuration in YAML
type YAMLMDNSConfig struct {
	ServiceName string        `yaml:"service_name"`
	Interval    time.Duration `yaml:"interval"`
}

// YAMLDHTConfig represents DHT configuration in YAML
type YAMLDHTConfig struct {
	RendezvousString  string        `yaml:"rendezvous_string"`
	DiscoveryInterval time.Duration `yaml:"discovery_interval"`
	Mode              string        `yaml:"mode"`
}

// YAMLBootstrapConfig represents bootstrap configuration in YAML
type YAMLBootstrapConfig struct {
	Peers             []string      `yaml:"peers"`
	ConnectionTimeout time.Duration `yaml:"connection_timeout"`
	RetryInterval     time.Duration `yaml:"retry_interval"`
	MaxRetries        int           `yaml:"max_retries"`
}

// YAMLProtocolConfig represents protocol configuration in YAML
type YAMLProtocolConfig struct {
	MaxMessageSize int64         `yaml:"max_message_size"`
	ReadTimeout    time.Duration `yaml:"read_timeout"`
	WriteTimeout   time.Duration `yaml:"write_timeout"`
	HandlerTimeout time.Duration `yaml:"handler_timeout"`
}

// YAMLRateLimiterConfig represents rate limiter configuration in YAML
type YAMLRateLimiterConfig struct {
	Limit  int           `yaml:"limit"`
	Window time.Duration `yaml:"window"`
	TTL    time.Duration `yaml:"ttl"`
}

// YAMLEncryptionConfig represents encryption configuration in YAML
type YAMLEncryptionConfig struct {
	Enabled bool   `yaml:"enabled"`
	KeyPath string `yaml:"key_path"`
}

// LoadYAMLConfig loads configuration from a YAML file
func LoadYAMLConfig(path string) (*YAMLConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg YAMLConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &cfg, nil
}

// Load loads the YAML config and merges it with CLI overrides
func Load(overrides CLIOverrides) (*YAMLConfig, error) {
	// Start with defaults
	cfg := DefaultYAMLConfig()

	// Determine config path
	configPath := overrides.ConfigPath
	if configPath == "" {
		configPath = DefaultConfigPath
	}

	// Try to load YAML config file
	if _, err := os.Stat(configPath); err == nil {
		loadedCfg, err := LoadYAMLConfig(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load config: %w", err)
		}
		cfg = loadedCfg
	} else if configPath != DefaultConfigPath {
		// If explicit config path is provided but doesn't exist, error
		return nil, fmt.Errorf("config file not found: %s", configPath)
	}
	// If default config path doesn't exist, use defaults silently

	// CLI flags override YAML config
	if overrides.StorageRoot != "" {
		cfg.StorageRoot = overrides.StorageRoot
	}
	if overrides.PeerWait != 0 {
		cfg.PeerWait = overrides.PeerWait
	}
	if overrides.Timeout != 0 {
		cfg.Timeout = overrides.Timeout
	}
	if overrides.LogFile != "" {
		cfg.LogFile = overrides.LogFile
	}
	if overrides.DiscoveryModes != "" {
		methods := []string{}
		for _, m := range strings.Split(overrides.DiscoveryModes, ",") {
			m = strings.TrimSpace(strings.ToLower(m))
			if m != "" {
				methods = append(methods, m)
			}
		}
		if len(methods) > 0 {
			cfg.Node.Discovery.EnabledMethods = methods
		}
	}
	if len(overrides.BootstrapPeers) > 0 {
		cfg.Node.Discovery.Bootstrap.Peers = overrides.BootstrapPeers
		// Automatically enable bootstrap method if peers provided
		hasBootstrap := false
		for _, m := range cfg.Node.Discovery.EnabledMethods {
			if m == "bootstrap" {
				hasBootstrap = true
				break
			}
		}
		if !hasBootstrap {
			cfg.Node.Discovery.EnabledMethods = append(cfg.Node.Discovery.EnabledMethods, "bootstrap")
		}
	}

	return cfg, nil
}

// DefaultYAMLConfig returns default YAML configuration
func DefaultYAMLConfig() *YAMLConfig {
	nodeCfg := node.DefaultConfig()
	protoCfg := protocol.DefaultConfig()
	rateCfg := middleware.DefaultRateLimiterConfig()
	discoveryCfg := discovery.DefaultConfig()

	return &YAMLConfig{
		StorageRoot: "./storage",
		LogFile:     "",
		PeerWait:    5 * time.Second,
		Timeout:     5 * time.Minute,

		Node: YAMLNodeConfig{
			ListenPort:         nodeCfg.ListenPort,
			IdentityPath:       nodeCfg.IdentityPath,
			MinConnection:      nodeCfg.MinConnection,
			MaxConnection:      nodeCfg.MaxConnection,
			Concurrency:        nodeCfg.Concurrency,
			EnableRelay:        nodeCfg.EnableRelay,
			EnableHolePunch:    nodeCfg.EnableHolePunch,
			EnableAutoNAT:      nodeCfg.EnableAutoNAT,
			RelayServers:       nodeCfg.RelayServers,
			EnableRelayService: nodeCfg.EnableRelayService,
			ExternalIP:         nodeCfg.ExternalIP,
			Discovery: YAMLDiscoveryConfig{
				EnabledMethods: []string{"mdns"},
				MDNS: YAMLMDNSConfig{
					ServiceName: discoveryCfg.MDNS.ServiceName,
					Interval:    discoveryCfg.MDNS.Interval,
				},
				DHT: YAMLDHTConfig{
					RendezvousString:  discoveryCfg.DHT.RendezvousString,
					DiscoveryInterval: discoveryCfg.DHT.DiscoveryInterval,
					Mode:              discoveryCfg.DHT.Mode,
				},
				Bootstrap: YAMLBootstrapConfig{
					Peers:             discoveryCfg.Bootstrap.BootstrapPeers,
					ConnectionTimeout: discoveryCfg.Bootstrap.ConnectionTimeout,
					RetryInterval:     discoveryCfg.Bootstrap.RetryInterval,
					MaxRetries:        discoveryCfg.Bootstrap.MaxRetries,
				},
			},
		},
		Protocol: YAMLProtocolConfig{
			MaxMessageSize: protoCfg.MaxMessageSize,
			ReadTimeout:    protoCfg.ReadTimeout,
			WriteTimeout:   protoCfg.WriteTimeout,
			HandlerTimeout: protoCfg.HandlerTimeout,
		},
		RateLimiter: YAMLRateLimiterConfig{
			Limit:  rateCfg.Limit,
			Window: rateCfg.Window,
			TTL:    rateCfg.TTL,
		},
		Encryption: YAMLEncryptionConfig{
			Enabled: true,
			KeyPath: "./encryption.key",
		},
	}
}

// ToNodeConfig converts YAML config to node.Config
func (y *YAMLConfig) ToNodeConfig() node.Config {
	cfg := node.DefaultConfig()

	if y.Node.ListenPort != 0 {
		cfg.ListenPort = y.Node.ListenPort
	}
	if y.Node.IdentityPath != "" {
		cfg.IdentityPath = y.Node.IdentityPath
	}
	if y.Node.MinConnection != 0 {
		cfg.MinConnection = y.Node.MinConnection
	}
	if y.Node.MaxConnection != 0 {
		cfg.MaxConnection = y.Node.MaxConnection
	}
	if y.Node.Concurrency != 0 {
		cfg.Concurrency = y.Node.Concurrency
	}

	// NAT Traversal settings
	cfg.EnableRelay = y.Node.EnableRelay
	cfg.EnableHolePunch = y.Node.EnableHolePunch
	cfg.EnableAutoNAT = y.Node.EnableAutoNAT
	cfg.RelayServers = y.Node.RelayServers
	cfg.EnableRelayService = y.Node.EnableRelayService
	cfg.ExternalIP = y.Node.ExternalIP

	cfg.DiscoveryConfig = y.ToDiscoveryConfig()

	return cfg
}

// ToDiscoveryConfig converts YAML config to discovery.Config
func (y *YAMLConfig) ToDiscoveryConfig() discovery.Config {
	cfg := discovery.DefaultConfig()

	// Parse enabled methods
	if len(y.Node.Discovery.EnabledMethods) > 0 {
		methods := []discovery.DiscoveryMethod{}
		for _, m := range y.Node.Discovery.EnabledMethods {
			switch m {
			case "mdns":
				methods = append(methods, discovery.MethodMDNS)
			case "dht":
				methods = append(methods, discovery.MethodDHT)
			case "bootstrap":
				methods = append(methods, discovery.MethodBootstrap)
			}
		}
		if len(methods) > 0 {
			cfg.EnabledMethods = methods
		}
	}

	// MDNS config
	if y.Node.Discovery.MDNS.ServiceName != "" {
		cfg.MDNS.ServiceName = y.Node.Discovery.MDNS.ServiceName
	}
	if y.Node.Discovery.MDNS.Interval != 0 {
		cfg.MDNS.Interval = y.Node.Discovery.MDNS.Interval
	}

	// DHT config
	if y.Node.Discovery.DHT.RendezvousString != "" {
		cfg.DHT.RendezvousString = y.Node.Discovery.DHT.RendezvousString
	}
	if y.Node.Discovery.DHT.DiscoveryInterval != 0 {
		cfg.DHT.DiscoveryInterval = y.Node.Discovery.DHT.DiscoveryInterval
	}
	if y.Node.Discovery.DHT.Mode != "" {
		cfg.DHT.Mode = y.Node.Discovery.DHT.Mode
	}

	// Bootstrap config
	if len(y.Node.Discovery.Bootstrap.Peers) > 0 {
		cfg.Bootstrap.BootstrapPeers = y.Node.Discovery.Bootstrap.Peers
	}
	if y.Node.Discovery.Bootstrap.ConnectionTimeout != 0 {
		cfg.Bootstrap.ConnectionTimeout = y.Node.Discovery.Bootstrap.ConnectionTimeout
	}
	if y.Node.Discovery.Bootstrap.RetryInterval != 0 {
		cfg.Bootstrap.RetryInterval = y.Node.Discovery.Bootstrap.RetryInterval
	}
	if y.Node.Discovery.Bootstrap.MaxRetries != 0 {
		cfg.Bootstrap.MaxRetries = y.Node.Discovery.Bootstrap.MaxRetries
	}

	return cfg
}

// ToProtocolConfig converts YAML config to protocol.Config
func (y *YAMLConfig) ToProtocolConfig() protocol.Config {
	cfg := protocol.DefaultConfig()

	if y.Protocol.MaxMessageSize != 0 {
		cfg.MaxMessageSize = y.Protocol.MaxMessageSize
	}
	if y.Protocol.ReadTimeout != 0 {
		cfg.ReadTimeout = y.Protocol.ReadTimeout
	}
	if y.Protocol.WriteTimeout != 0 {
		cfg.WriteTimeout = y.Protocol.WriteTimeout
	}
	if y.Protocol.HandlerTimeout != 0 {
		cfg.HandlerTimeout = y.Protocol.HandlerTimeout
	}

	return cfg
}

// ToRateLimiterConfig converts YAML config to middleware.RateLimiterConfig
func (y *YAMLConfig) ToRateLimiterConfig() middleware.RateLimiterConfig {
	cfg := middleware.DefaultRateLimiterConfig()

	if y.RateLimiter.Limit != 0 {
		cfg.Limit = y.RateLimiter.Limit
	}
	if y.RateLimiter.Window != 0 {
		cfg.Window = y.RateLimiter.Window
	}
	if y.RateLimiter.TTL != 0 {
		cfg.TTL = y.RateLimiter.TTL
	}

	return cfg
}

// ToEncryptionConfig converts YAML config to store.EncryptionConfig
func (y *YAMLConfig) ToEncryptionConfig() store.EncryptionConfig {
	return store.EncryptionConfig{
		Enabled: y.Encryption.Enabled,
		KeyPath: y.Encryption.KeyPath,
	}
}

// ToConfig converts YAML config to internal Config struct
func (y *YAMLConfig) ToConfig() Config {
	return Config{
		NodeConfig:        y.ToNodeConfig(),
		ProtocolConfig:    y.ToProtocolConfig(),
		RateLimiterConfig: y.ToRateLimiterConfig(),
		Encryption:        y.ToEncryptionConfig(),
	}
}
