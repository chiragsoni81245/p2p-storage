package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/config"
	"github.com/chiragsoni81245/p2p-storage/internal/fileserver"
	"github.com/chiragsoni81245/p2p-storage/internal/store"
	"github.com/spf13/cobra"
)

var (
	// Global flags
	configPath     string
	storageRoot    string
	peerWait       time.Duration
	timeout        time.Duration
	logFile        string
	logLevel       string
	discoveryModes string
	bootstrapPeers []string

	// Loaded YAML config (merged with defaults)
	yamlConfig *config.YAMLConfig
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// rootCmd represents the base command
var rootCmd = &cobra.Command{
	Use:   "p2p-storage",
	Short: "P2P distributed file storage CLI",
	Long: `A peer-to-peer distributed file storage system.

Store and retrieve files across a decentralized network of peers.
Files are content-addressed using SHA256 hashes.`,
}

// storeCmd stores a file to the network
var storeCmd = &cobra.Command{
	Use:   "store <filepath>",
	Short: "Store a file in the P2P network",
	Long: `Store a local file to the P2P network.

The file will be distributed to all connected peers.
Returns the file key which can be used to retrieve it later.`,
	Args:         cobra.ExactArgs(1),
	RunE:         runStore,
	SilenceUsage: true,
	Example: `  p2p-storage store ./myfile.txt
  p2p-storage store -s ./data ./document.pdf`,
}

// getCmd retrieves a file from the network
var getCmd = &cobra.Command{
	Use:   "get <filekey> [output-path]",
	Short: "Get a file from the P2P network",
	Long: `Retrieve a file from the P2P network using its key.

If the file exists locally, it will be copied to the output path.
Otherwise, it will be fetched from connected peers.`,
	Args:         cobra.RangeArgs(1, 2),
	RunE:         runGet,
	SilenceUsage: true,
	Example: `  p2p-storage get abc123...def ./output.txt
  p2p-storage get abc123...def`,
}

// getFileKeyCmd gets the key for a file without storing
var getFileKeyCmd = &cobra.Command{
	Use:   "get-file-key <filepath>",
	Short: "Get the storage key for a file",
	Long: `Calculate and display the key for a file without storing it.

This key can be used to retrieve the file from the network
after it has been stored by any peer.`,
	Args:         cobra.ExactArgs(1),
	RunE:         runGetFileKey,
	SilenceUsage: true,
	Example:      `  p2p-storage get-file-key ./myfile.txt`,
}

// daemonCmd runs the node as a daemon
var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run as a background daemon",
	Long: `Start the P2P node and keep it running.

The daemon will accept connections from other peers
and serve stored files on request.`,
	RunE:         runDaemon,
	SilenceUsage: true,
	Example: `  p2p-storage daemon
  p2p-storage -s ./data daemon`,
}

func init() {
	// Global persistent flags
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", config.DefaultConfigPath, "path to config file")
	rootCmd.PersistentFlags().StringVarP(&storageRoot, "storage", "s", "", "storage directory (overrides config file)")
	rootCmd.PersistentFlags().DurationVarP(&peerWait, "wait", "w", 0, "time to wait for peer discovery (overrides config file)")
	rootCmd.PersistentFlags().DurationVarP(&timeout, "timeout", "t", 0, "operation timeout (overrides config file)")
	rootCmd.PersistentFlags().StringVarP(&logFile, "log-file", "l", "", "path to log file (overrides config file)")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "", "log level: debug, info, error (overrides config file)")
	rootCmd.PersistentFlags().StringVarP(&discoveryModes, "discovery", "d", "", "discovery methods (comma-separated: mdns,dht,bootstrap) (overrides config file)")
	rootCmd.PersistentFlags().StringSliceVarP(&bootstrapPeers, "bootstrap", "b", []string{}, "bootstrap peer addresses (multiaddr format) (overrides config file)")

	// Load config before any command runs
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		var err error
		yamlConfig, err = config.Load(config.CLIOverrides{
			ConfigPath:     configPath,
			StorageRoot:    storageRoot,
			PeerWait:       peerWait,
			Timeout:        timeout,
			LogFile:        logFile,
			LogLevel:       logLevel,
			DiscoveryModes: discoveryModes,
			BootstrapPeers: bootstrapPeers,
		})
		return err
	}

	// Add commands
	rootCmd.AddCommand(storeCmd)
	rootCmd.AddCommand(getCmd)
	rootCmd.AddCommand(getFileKeyCmd)
	rootCmd.AddCommand(daemonCmd)
}

// getLogWriter returns the appropriate log writer based on config
func getLogWriter() (io.Writer, func(), error) {
	if yamlConfig.LogFile == "" {
		return os.Stdout, func() {}, nil
	}

	f, err := os.OpenFile(yamlConfig.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open log file: %w", err)
	}

	return f, func() { f.Close() }, nil
}

// startFileServer creates and starts the file server
func startFileServer() (*fileserver.FileServer, func(), error) {
	logWriter, closeLog, err := getLogWriter()
	if err != nil {
		return nil, nil, err
	}

	fs, err := fileserver.NewFileServer(fileserver.FileServerOpts{
		StorageRoot:       yamlConfig.StorageRoot,
		PathTransformFunc: store.CASPathTransformFunc,
		LogWriter:         logWriter,
		LogLevel:          yamlConfig.LogLevel,
		Config:            yamlConfig.ToConfig(),
	})
	if err != nil {
		closeLog()
		return nil, nil, fmt.Errorf("failed to create file server: %w", err)
	}

	if err := fs.Start(); err != nil {
		closeLog()
		return nil, nil, fmt.Errorf("failed to start node: %w", err)
	}

	fmt.Printf("Node ID: %s\n", fs.GetNodeID().String())
	for _, addr := range fs.GetNodeAddresses() {
		fmt.Printf("Address: %s\n", addr)
	}

	return fs, closeLog, nil
}

// waitForPeers waits for peer discovery
func waitForPeers(fs *fileserver.FileServer) int {
	fmt.Printf("Discovering peers (%s)...\n", yamlConfig.PeerWait)

	deadline := time.Now().Add(yamlConfig.PeerWait)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		<-ticker.C
		if count := fs.GetConnectedPeers(); count > 0 {
			fmt.Printf("Found %d peer(s)\n", count)
			return count
		}
	}

	count := fs.GetConnectedPeers()
	if count == 0 {
		fmt.Println("No peers found")
	} else {
		fmt.Printf("Found %d peer(s)\n", count)
	}
	return count
}

func runStore(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	// Check file exists
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %s", filePath)
	}
	if info.IsDir() {
		return fmt.Errorf("cannot store a directory: %s", filePath)
	}

	// Generate file key
	key, err := store.GenerateFileKey(filePath)
	if err != nil {
		return err
	}

	fmt.Printf("File: %s\n", filepath.Base(filePath))
	fmt.Printf("Size: %d bytes\n", info.Size())
	fmt.Printf("Key:  %s\n\n", key)

	// Start the file server
	fs, closeLog, err := startFileServer()
	if err != nil {
		return err
	}
	defer closeLog()
	defer fs.Stop()

	// Wait for peers (but don't fail if none - file will still be stored locally)
	peerCount := waitForPeers(fs)

	fmt.Println("\nStoring file...")

	ctx, cancel := context.WithTimeout(context.Background(), yamlConfig.Timeout)
	defer cancel()

	count, err := fs.StoreFile(ctx, key, filePath, peerCount)
	if err != nil {
		return fmt.Errorf("failed to store file: %w", err)
	}

	fmt.Println("\n✓ Stored locally")
	if count > 0 {
		fmt.Printf("✓ Replicated to %d peer(s)\n", count)
	} else if peerCount == 0 {
		fmt.Println("⚠ No peers available for replication")
	}
	fmt.Printf("\nTo retrieve this file, use:\n  p2p-storage get %s\n", key)

	return nil
}

func runGet(cmd *cobra.Command, args []string) error {
	key := args[0]

	// Determine output path
	outputPath := key[:16] + ".dat"
	if len(args) >= 2 {
		outputPath = args[1]
	}

	// Start the file server
	fs, closeLog, err := startFileServer()
	if err != nil {
		return err
	}
	defer closeLog()
	defer fs.Stop()

	// Check if file exists locally, otherwise wait for peers
	if !fs.HasFile(key) {
		peerCount := waitForPeers(fs)
		if peerCount == 0 {
			return fmt.Errorf("no peers available and file not found locally")
		}
		fmt.Printf("\nRequesting from %d peer(s)...\n", peerCount)
	}

	ctx, cancel := context.WithTimeout(context.Background(), yamlConfig.Timeout)
	defer cancel()

	if err := fs.GetFile(ctx, key, outputPath); err != nil {
		return fmt.Errorf("failed to get file: %w", err)
	}

	fmt.Printf("\n✓ Saved to: %s\n", outputPath)

	return nil
}

func runGetFileKey(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	// Check file exists
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %s", filePath)
	}
	if info.IsDir() {
		return fmt.Errorf("cannot get key for a directory: %s", filePath)
	}

	key, err := store.GenerateFileKey(filePath)
	if err != nil {
		return err
	}

	// Just print the key (good for scripting)
	fmt.Println(key)

	return nil
}

func runDaemon(cmd *cobra.Command, args []string) error {
	fmt.Println("Starting P2P Storage daemon...")

	fs, closeLog, err := startFileServer()
	if err != nil {
		return err
	}
	defer closeLog()
	defer fs.Stop()

	fmt.Printf("Storage: %s\n", yamlConfig.StorageRoot)
	fmt.Println("\nDaemon running. Press Ctrl+C to stop.")

	// Print status periodically
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			fmt.Printf("[%s] Peers: %d\n",
				time.Now().Format("15:04:05"),
				fs.GetConnectedPeers())
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\nShutting down...")
	return nil
}
