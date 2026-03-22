package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/config"
	"github.com/chiragsoni81245/p2p-storage/internal/fileserver"
	"github.com/chiragsoni81245/p2p-storage/internal/middleware"
	"github.com/chiragsoni81245/p2p-storage/internal/node"
	"github.com/chiragsoni81245/p2p-storage/internal/protocol"
	"github.com/chiragsoni81245/p2p-storage/internal/store"
	"github.com/spf13/cobra"
)

var (
	// Global flags
	configPath  string
	storageRoot string
	peerWait    time.Duration
	timeout     time.Duration
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
	Args: cobra.ExactArgs(1),
	RunE: runStore,
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
	Args: cobra.RangeArgs(1, 2),
	RunE: runGet,
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
	Args:    cobra.ExactArgs(1),
	RunE:    runGetFileKey,
	Example: `  p2p-storage get-file-key ./myfile.txt`,
}

// daemonCmd runs the node as a daemon
var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run as a background daemon",
	Long: `Start the P2P node and keep it running.

The daemon will accept connections from other peers
and serve stored files on request.`,
	RunE: runDaemon,
	Example: `  p2p-storage daemon
  p2p-storage -s ./data daemon`,
}

func init() {
	// Global persistent flags
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "path to config file")
	rootCmd.PersistentFlags().StringVarP(&storageRoot, "storage", "s", "./storage", "storage directory")
	rootCmd.PersistentFlags().DurationVarP(&peerWait, "wait", "w", 5*time.Second, "time to wait for peer discovery")
	rootCmd.PersistentFlags().DurationVarP(&timeout, "timeout", "t", 5*time.Minute, "operation timeout")

	// Add commands
	rootCmd.AddCommand(storeCmd)
	rootCmd.AddCommand(getCmd)
	rootCmd.AddCommand(getFileKeyCmd)
	rootCmd.AddCommand(daemonCmd)
}

// startFileServer creates and starts the file server
func startFileServer() (*fileserver.FileServer, error) {
	fs := fileserver.NewFileServer(fileserver.FileServerOpts{
		StorageRoot:       storageRoot,
		PathTransformFunc: store.CASPathTransformFunc,
		Config: config.Config{
			NodeConfig:        node.DefaultConfig(),
			ProtocolConfig:    protocol.DefaultConfig(),
			RateLimiterConfig: middleware.DefaultRateLimiterConfig(),
		},
	})

	if err := fs.Start(); err != nil {
		return nil, fmt.Errorf("failed to start node: %w", err)
	}

	fmt.Printf("Node ID: %s\n", fs.GetNodeID().String())
	for _, addr := range fs.GetNodeAddresses() {
		fmt.Printf("Address: %s\n", addr)
	}

	return fs, nil
}

// waitForPeers waits for peer discovery
func waitForPeers(fs *fileserver.FileServer) int {
	fmt.Printf("Discovering peers (%s)...\n", peerWait)

	deadline := time.Now().Add(peerWait)
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
	fs, err := startFileServer()
	if err != nil {
		return err
	}
	defer fs.Stop()

	// Wait for peers
	peerCount := waitForPeers(fs)
	if peerCount == 0 {
		return fmt.Errorf("no peers available to store file")
	}

	fmt.Printf("\nStoring to %d peer(s)...\n", peerCount)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	count, err := fs.StoreFileToNetwork(ctx, key, filePath, peerCount)
	if err != nil {
		return fmt.Errorf("failed to store file: %w", err)
	}

	fmt.Printf("\n✓ Stored to %d peer(s)\n", count)
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
	fs, err := startFileServer()
	if err != nil {
		return err
	}
	defer fs.Stop()

	// Check if file exists locally
	if fs.HasFile(key) {
		fmt.Println("File found locally")
		return nil
	} else {
		// Wait for peers
		peerCount := waitForPeers(fs)
		if peerCount == 0 {
			return fmt.Errorf("no peers available and file not found locally")
		}
		fmt.Printf("\nRequesting from %d peer(s)...\n", peerCount)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := fs.GetFileFromNetwork(ctx, key, outputPath); err != nil {
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

	fs, err := startFileServer()
	if err != nil {
		return err
	}
	defer fs.Stop()

	fmt.Printf("Storage: %s\n", storageRoot)
	fmt.Println("\nDaemon running. Press Ctrl+C to stop.\n")

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
