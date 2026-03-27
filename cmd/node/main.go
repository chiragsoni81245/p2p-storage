package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/config"
	"github.com/chiragsoni81245/p2p-storage/internal/fileserver"
	"github.com/chiragsoni81245/p2p-storage/internal/store"
	"github.com/libp2p/go-libp2p/core/peer"
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

// Flags for send command
var (
	allowRelay    bool
	holePunchWait time.Duration
)

// sendCmd sends a file directly to a specific peer
var sendCmd = &cobra.Command{
	Use:   "send <filepath> <peer-multiaddr>",
	Short: "Send a file directly to a specific peer",
	Long: `Send a file to a specific peer using their multiaddr.

By default, this command only uses direct connections (no relay).
If both you and the peer are behind NAT, hole punching will be attempted.
If direct connection cannot be established, the transfer will fail.

Use --allow-relay to permit relayed transfers (not recommended for sensitive data).`,
	Args:         cobra.ExactArgs(2),
	RunE:         runSend,
	SilenceUsage: true,
	Example: `  # Send to a peer (direct connection only)
  p2p-storage send ./myfile.txt /ip4/192.168.1.100/tcp/4001/p2p/12D3KooW...

  # Send with longer hole punch wait time
  p2p-storage send --hole-punch-wait 30s ./myfile.txt /ip4/.../p2p/12D3KooW...

  # Allow relayed transfer (not recommended for sensitive data)
  p2p-storage send --allow-relay ./myfile.txt /ip4/.../p2p/12D3KooW...`,
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
	rootCmd.AddCommand(sendCmd)

	// Flags for send command
	sendCmd.Flags().BoolVar(&allowRelay, "allow-relay", false, "allow transfer over relayed connections (not recommended)")
	sendCmd.Flags().DurationVar(&holePunchWait, "hole-punch-wait", 10*time.Second, "time to wait for hole punching to establish direct connection")
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

func runSend(cmd *cobra.Command, args []string) error {
	filePath := args[0]
	peerAddr := args[1]

	// Check file exists
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %s", filePath)
	}
	if info.IsDir() {
		return fmt.Errorf("cannot send a directory: %s", filePath)
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

	// Store file locally first
	if !fs.HasFile(key) {
		fmt.Println("Storing file locally...")
		ctx, cancel := context.WithTimeout(context.Background(), yamlConfig.Timeout)
		_, err := fs.StoreFile(ctx, key, filePath, 0) // 0 = don't replicate yet
		cancel()
		if err != nil {
			return fmt.Errorf("failed to store file locally: %w", err)
		}
	}

	var peerID peer.ID
	ctx, cancel := context.WithTimeout(context.Background(), yamlConfig.Timeout)

	// Check if input address is relayed one
	if !strings.Contains(peerAddr, "p2p-circuit") {
		// if its direct then try direct connection and it not able to connect to error out
		fmt.Printf("Connecting to peer: %s\n", peerAddr)
		peerID, err = fs.ConnectToPeer(ctx, peerAddr)
		if err != nil {
			return fmt.Errorf("failed to connect to peer: %w", err)
		}
		fmt.Printf("Connected to: %s\n", peerID.String())
	} else {
		// if its relayed

		// connect to this specific relay
		relayAddr := strings.Split(peerAddr, "/p2p-circuit/")[0]

		fmt.Printf("Connecting to relay: %s\n", relayAddr)
		relayPeerID, err := fs.ConnectToPeer(ctx, relayAddr)
		if err != nil {
			return fmt.Errorf("failed to connect to peer: %w", err)
		}
		fmt.Printf("Connected to relay: %s\n", relayPeerID.String())

		// wait for the reservation on relay by keep checking the address
		ticker := time.NewTicker(5 * time.Second)
		foundRelayReservation := false

		relayReservationWaitLoop:
		for {
			select {
			case <-ctx.Done():
            	return fmt.Errorf("timed out waiting for relay reservation after %s", yamlConfig.Timeout)
			case <-ticker.C:
				for _, addr := range fs.GetNodeAddresses() {
					if strings.Contains(addr, relayPeerID.String()) {
						fmt.Println("✓ Relay reservation ACTIVE:", addr)
						foundRelayReservation = true
					}
				}
				if !foundRelayReservation {
					fmt.Println("✗ No relay reservation yet")
					continue
				}
				break relayReservationWaitLoop
			}
		}
		ticker.Stop()

		// connect to the peer through this relay
		fmt.Printf("Connecting to peer through relay for direct connection negotiation: %s\n", peerAddr)
		peerID, err = fs.ConnectToPeer(ctx, peerAddr)
		if err != nil {
			return fmt.Errorf("failed to connect to peer through relay: %w", err)
		}
		fmt.Printf("Connected to peer through relay: %s\n", peerID.String())
		cancel()

		if !allowRelay {
			// wait for that peer to be connect with direct connection
			fmt.Printf("\nWaiting for hole punching (%s)...\n", holePunchWait)
			ctx, cancel := context.WithTimeout(context.Background(), holePunchWait)
			err := fs.WaitForDirectConnection(ctx, peerID, holePunchWait)
			cancel()

			if err != nil {
				// Check one more time
				if fs.IsDirectConnection(peerID) {
					fmt.Println("Direct connection established!")
				} else {
					return fmt.Errorf("cannot establish direct connection to peer (only relayed connection available)\n" +
					"Use --allow-relay to permit relayed transfers (not recommended for sensitive data)")
				}
			} else {
				fmt.Println("Direct connection established via hole punching!")
			}
		} else {
			fmt.Println("\n⚠ WARNING: Using relayed connection (encrypted data will pass through relay server)")
		}
	}

	// Send the file
	fmt.Println("\nSending file...")
	ctx, cancel = context.WithTimeout(context.Background(), yamlConfig.Timeout)
	defer cancel()

	if allowRelay {
		err = fs.StoreFileToPeer(ctx, peerID, key)
	} else {
		err = fs.StoreFileToPeerDirect(ctx, peerID, key)
	}

	if err != nil {
		if err == fileserver.ErrRelayedConnection {
			return fmt.Errorf("connection became relayed during transfer - aborting for security")
		}
		return fmt.Errorf("failed to send file: %w", err)
	}

	fmt.Println("\n✓ File sent successfully!")
	fmt.Printf("  To peer: %s\n", peerID.String())
	fmt.Printf("  Key: %s\n", key)

	return nil
}
