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
	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/chiragsoni81245/p2p-storage/internal/fileserver"
	"github.com/chiragsoni81245/p2p-storage/pkg/operations"
	"github.com/spf13/cobra"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
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

// storeLocallyCmd stores a file to local storage only
var storeLocallyCmd = &cobra.Command{
	Use:   "store <filepath>",
	Short: "Store a file in local storage",
	Long: `Store a local file in this node's storage.

The file is stored locally only and not sent to any peers.
Returns the file key which can be used to retrieve it later.`,
	Args:         cobra.ExactArgs(1),
	RunE:         runStoreLocally,
	SilenceUsage: true,
	Example: `  p2p-storage store ./myfile.txt`,
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

// Flags for send command
var (
	allowRelay    bool
	holePunchWait time.Duration
	sendSession   string
)

// Flags for receive command
var receiveSession string

// receiveCmd starts a session and waits for incoming file transfers
var receiveCmd = &cobra.Command{
	Use:   "receive",
	Short: "Wait to receive files from peers",
	Long: `Start a receive session and wait for incoming file transfers.

Only senders that supply the correct --session name will be accepted.
The node's addresses are printed so you can share them with the sender.
Press Ctrl+C to stop.`,
	RunE:         runReceive,
	SilenceUsage: true,
	Example: `  p2p-storage receive --session mysession`,
}

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
	rootCmd.AddCommand(storeLocallyCmd)
	rootCmd.AddCommand(getCmd)
	rootCmd.AddCommand(getFileKeyCmd)
	rootCmd.AddCommand(sendCmd)
	rootCmd.AddCommand(receiveCmd)

	// Flags for send command
	sendCmd.Flags().BoolVar(&allowRelay, "allow-relay", false, "allow transfer over relayed connections (not recommended)")
	sendCmd.Flags().DurationVar(&holePunchWait, "hole-punch-wait", 10*time.Second, "time to wait for hole punching to establish direct connection")
	sendCmd.Flags().StringVar(&sendSession, "session", "", "session name required by the receiver (optional)")

	// Flags for receive command
	receiveCmd.Flags().StringVar(&receiveSession, "session", "", "session name that senders must supply (required)")
	_ = receiveCmd.MarkFlagRequired("session")
}

// getLogWriter returns the appropriate log writer based on config.
func getLogWriter() (*os.File, error) {
	if yamlConfig.LogFile == "" {
		return os.Stdout, nil
	}
	return os.OpenFile(yamlConfig.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
}

// startServer starts a FileServer and prints node identity info to stdout.
func startServer() (*fileserver.FileServer, func(), error) {
	logWriter, err := getLogWriter()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open log file: %w", err)
	}

	closeLog := func() {}
	if logWriter != os.Stdout {
		closeLog = func() { logWriter.Close() }
	}

	fs, err := operations.StartServer(yamlConfig, logWriter)
	if err != nil {
		closeLog()
		return nil, nil, err
	}

	fmt.Printf("Node ID: %s\n", fs.GetNodeID().String())
	for _, addr := range fs.GetNodeAddresses() {
		fmt.Printf("Address: %s\n", addr)
	}

	return fs, closeLog, nil
}

func runStoreLocally(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %s", filePath)
	}
	if info.IsDir() {
		return fmt.Errorf("cannot store a directory: %s", filePath)
	}
	fmt.Printf("File: %s\n", filepath.Base(filePath))
	fmt.Printf("Size: %d bytes\n\n", info.Size())

	fs, closeLog, err := startServer()
	if err != nil {
		return err
	}
	defer closeLog()
	defer fs.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), yamlConfig.Timeout)
	defer cancel()

	result, err := operations.StoreLocally(ctx, fs, filePath)
	if err != nil {
		return err
	}

	fmt.Printf("Key:  %s\n\n", result.Key)
	fmt.Println("✓ Stored locally")
	fmt.Printf("\nTo retrieve this file, use:\n  p2p-storage get %s\n", result.Key)

	return nil
}

func runGet(cmd *cobra.Command, args []string) error {
	key := args[0]

	outputPath := key[:16] + ".dat"
	if len(args) >= 2 {
		outputPath = args[1]
	}

	fs, closeLog, err := startServer()
	if err != nil {
		return err
	}
	defer closeLog()
	defer fs.Stop()

	// Subscribe before firing the operation so no events are missed
	bus := fs.GetBus()
	progressCh := bus.Subscribe(event.FileGetProgress)
	defer bus.Unsubscribe(event.FileGetProgress, progressCh)

	// Print progress updates in background
	requestID := key
	go func() {
		for evt := range progressCh {
			if evt.RequestID != requestID {
				continue
			}
			d := evt.Data.(event.GetProgressData)
			if d.TotalBytes > 0 {
				pct := float64(d.BytesReceived) / float64(d.TotalBytes) * 100
				fmt.Printf("\rReceiving... %.0f%%", pct)
			} else {
				fmt.Print("\rReceiving...")
			}
		}
	}()

	fmt.Printf("Discovering peers (%s)...\n", yamlConfig.PeerWait)

	ctx, cancel := context.WithTimeout(context.Background(), yamlConfig.Timeout)
	defer cancel()

	operations.GetFile(fs, yamlConfig, key, requestID, bus)

	if _, err := operations.WaitForGet(ctx, bus, requestID); err != nil {
		return fmt.Errorf("failed to get file: %w", err)
	}

	// File is now in local storage — copy it to the requested output path
	if err := fs.WriteFileTo(key, outputPath); err != nil {
		return fmt.Errorf("failed to save file: %w", err)
	}

	fmt.Printf("\n✓ Saved to: %s\n", outputPath)
	return nil
}

func runGetFileKey(cmd *cobra.Command, args []string) error {
	key, err := operations.GetFileKey(args[0])
	if err != nil {
		return err
	}
	fmt.Println(key)
	return nil
}

func runSend(cmd *cobra.Command, args []string) error {
	filePath := args[0]
	peerAddr := args[1]

	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %s", filePath)
	}
	if info.IsDir() {
		return fmt.Errorf("cannot send a directory: %s", filePath)
	}
	fmt.Printf("File: %s\n", filepath.Base(filePath))
	fmt.Printf("Size: %d bytes\n\n", info.Size())

	fs, closeLog, err := startServer()
	if err != nil {
		return err
	}
	defer closeLog()
	defer fs.Stop()

	fmt.Println("Sending file...")

	ctx, cancel := context.WithTimeout(context.Background(), yamlConfig.Timeout*3)
	defer cancel()

	result, err := operations.SendFile(ctx, fs, yamlConfig, filePath, peerAddr, operations.SendOpts{
		AllowRelay:    allowRelay,
		HolePunchWait: holePunchWait,
		Session:       sendSession,
	})
	if err != nil {
		return err
	}

	fmt.Println("\n✓ File sent successfully!")
	fmt.Printf("  To peer: %s\n", result.PeerID)
	fmt.Printf("  Key: %s\n", result.Key)

	return nil
}

func runReceive(cmd *cobra.Command, args []string) error {
	fs, closeLog, err := startServer()
	if err != nil {
		return err
	}
	defer closeLog()
	defer fs.Stop()

	fmt.Printf("Session: %s\n", receiveSession)
	fmt.Println("Waiting for incoming transfers... (Ctrl+C to stop)")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel on Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
	}()

	// One mpb progress container, shared across all files
	p := mpb.NewWithContext(ctx)
	bars := make(map[string]*mpb.Bar)

	operations.StartReceiveSession(ctx, fs, receiveSession, func(evt event.Event) {
		switch evt.Type {
		case event.FileReceiveStarted:
			d := evt.Data.(event.ReceiveStartedData)
			bar := p.AddBar(d.Size,
				mpb.PrependDecorators(
					decor.Name(d.Key[:min(16, len(d.Key))], decor.WC{C: decor.DindentRight | decor.DextraSpace}),
					decor.CountersKibiByte("% .2f / % .2f"),
				),
				mpb.AppendDecorators(decor.EwmaETA(decor.ET_STYLE_GO, 30)),
			)
			bars[d.Key] = bar
		case event.FileReceiveProgress:
			d := evt.Data.(event.ReceiveProgressData)
			if bar, ok := bars[d.Key]; ok {
				bar.SetCurrent(d.BytesReceived)
			}
		case event.FileReceiveComplete:
			d := evt.Data.(event.ReceiveCompleteData)
			if bar, ok := bars[d.Key]; ok {
				bar.SetTotal(d.Size, true)
				delete(bars, d.Key)
			}
			fmt.Printf("✓ Received: %s (%d bytes)\n", d.Key, d.Size)
		case event.FileReceiveFailed:
			d := evt.Data.(event.ReceiveFailedData)
			if bar, ok := bars[d.Key]; ok {
				bar.Abort(true)
				delete(bars, d.Key)
			}
			fmt.Printf("✗ Failed:   %s (%v)\n", d.Key, d.Err)
		}
	})

	p.Wait()
	return nil
}
