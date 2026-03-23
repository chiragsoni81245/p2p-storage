package fileserver

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/chiragsoni81245/p2p-storage/internal/config"
	"github.com/chiragsoni81245/p2p-storage/internal/core"
	"github.com/chiragsoni81245/p2p-storage/internal/discovery"
	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/chiragsoni81245/p2p-storage/internal/middleware"
	"github.com/chiragsoni81245/p2p-storage/internal/network"
	"github.com/chiragsoni81245/p2p-storage/internal/node"
	"github.com/chiragsoni81245/p2p-storage/internal/observability"
	"github.com/chiragsoni81245/p2p-storage/internal/protocol"
	"github.com/chiragsoni81245/p2p-storage/internal/store"
	"github.com/libp2p/go-libp2p/core/host"
	libp2p_network "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

type FileServerOpts struct {
	StorageRoot       string
	PathTransformFunc store.PathTransformFunc
	LogWriter         io.Writer // Log output writer (defaults to os.Stdout if nil)
	LogLevel          string    // Log level: debug, info, error (defaults to "info" if empty)
	Config            config.Config
	Encryption        *store.EncryptionConfig // Encryption config (enabled by default if nil)
}

type Peer struct {
	ID   peer.ID
	Conn libp2p_network.Conn
}

type FileServer struct {
	FileServerOpts

	peerLock sync.Mutex
	peers    map[string]Peer

	store  *store.Store
	quitch chan struct{}

	node            host.Host
	logger          *observability.Logger
	metrics         *observability.Metrics
	bus             *event.Bus
	scorer          *network.PeerScorer
	transferHandler *FileTransferHandler
	proto           *protocol.Protocol
	discoveryMgr    *discovery.Manager

	// Transfer completion waiters
	waitersLock sync.Mutex
	waiters     map[string][]chan error
}

// Handle implements core.Handler for the storage protocol
func (fs *FileServer) Handle(ctx context.Context, peerID peer.ID, msg core.Message) (core.Message, error) {
	return fs.handleMessage(peerID, msg)
}

func NewFileServer(opts FileServerOpts) (*FileServer, error) {
	// Default encryption to enabled
	encryption := store.EncryptionConfig{Enabled: true}
	if opts.Encryption != nil {
		encryption = *opts.Encryption
	}

	storeOpts := store.StoreOpts{
		Root:              opts.StorageRoot,
		PathTransformFunc: opts.PathTransformFunc,
		Encryption:        encryption,
	}

	// Use provided log writer or default to stdout
	logWriter := opts.LogWriter
	if logWriter == nil {
		logWriter = os.Stdout
	}

	logLevel := observability.ParseLogLevel(opts.LogLevel)
	logger := observability.NewLoggerWithLevel(logWriter, observability.Fields{}, logLevel)
	metrics := observability.NewMetrics()
	bus := event.NewBus()
	scorer := network.NewPeerScorer() // Manage peer score and use best peers

	s, err := store.NewStore(storeOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create store: %w", err)
	}

	return &FileServer{
		FileServerOpts: opts,
		store:          s,
		quitch:         make(chan struct{}),
		peers:          make(map[string]Peer),
		logger:         logger,
		metrics:        metrics,
		bus:            bus,
		scorer:         scorer,
		waiters:        make(map[string][]chan error),
	}, nil
}

func (fs *FileServer) getPeer(id string) (Peer, error) {
	fs.peerLock.Lock()
	defer fs.peerLock.Unlock()

	peer, ok := fs.peers[id]
	if !ok {
		return Peer{}, fmt.Errorf("peer not found in peer list")
	}

	return peer, nil
}

// registerTransferWaiter registers a channel to be notified when a file transfer completes
func (fs *FileServer) registerTransferWaiter(key string) chan error {
	fs.waitersLock.Lock()
	defer fs.waitersLock.Unlock()

	ch := make(chan error, 1)
	fs.waiters[key] = append(fs.waiters[key], ch)
	return ch
}

// notifyTransferComplete notifies all waiters for a given key that the transfer is complete
func (fs *FileServer) notifyTransferComplete(key string, err error) {
	fs.waitersLock.Lock()
	defer fs.waitersLock.Unlock()

	waiters, ok := fs.waiters[key]
	if !ok {
		return
	}

	for _, ch := range waiters {
		select {
		case ch <- err:
		default:
		}
		close(ch)
	}
	delete(fs.waiters, key)
}

func (fs *FileServer) OnPeerConnect(conn libp2p_network.Conn) {
	fs.peerLock.Lock()
	defer fs.peerLock.Unlock()

	peerID := conn.RemotePeer().String()

	// Only add if not already tracked (avoid duplicate events)
	if _, exists := fs.peers[peerID]; !exists {
		fs.peers[peerID] = Peer{
			ID:   conn.RemotePeer(),
			Conn: conn,
		}
		fs.logger.Info("peer registered", observability.Fields{
			"peer_id":    peerID,
			"peer_count": len(fs.peers),
		})
	}
}

func (fs *FileServer) OnPeerDisconnect(conn libp2p_network.Conn) {
	fs.peerLock.Lock()
	defer fs.peerLock.Unlock()

	peerID := conn.RemotePeer().String()

	// Only remove if the peer is truly disconnected (no remaining connections)
	// Check using the host's network state
	if fs.node != nil && fs.node.Network().Connectedness(conn.RemotePeer()) == libp2p_network.Connected {
		// Still connected via another connection, don't remove
		return
	}

	delete(fs.peers, peerID)
	fs.logger.Info("peer unregistered", observability.Fields{
		"peer_id":    peerID,
		"peer_count": len(fs.peers),
	})
}

func (fs *FileServer) Start() error {
	node, err := node.NewNode(fs.Config.NodeConfig)
	if err != nil {
		return err
	}
	fs.node = node

	fs.logger.Info("Node started", observability.Fields{"node_id": node.ID().String()})
	for _, addr := range node.Addrs() {
		fs.logger.Info("peer address", observability.Fields{
			"address": fmt.Sprintf("%s/p2p/%s", addr, node.ID()),
		})
	}

	// Initial handler which will control app working
	var handler core.Handler = fs

	// Apply Rate Limiting
	rl := middleware.NewRateLimiter(
		fs.Config.RateLimiterConfig.Limit,
		fs.Config.RateLimiterConfig.Window,
		fs.Config.RateLimiterConfig.TTL,
	)
	handler = rl.Wrap(handler)

	limiter := middleware.NewLimiter(fs.Config.NodeConfig.Concurrency)

	fs.proto = protocol.New(node, "/storage/1.0.0", handler, fs.Config.ProtocolConfig, fs.logger, limiter)

	// Initialize the file transfer handler for streaming file data
	fs.transferHandler = NewFileTransferHandler(node, fs, fs.logger)

	// Subscribe BEFORE starting goroutines to avoid race condition
	// Events published before subscription is ready would be lost
	peerConnectCh := fs.bus.Subscribe(event.PeerConnected)
	peerDisconnectCh := fs.bus.Subscribe(event.PeerDisconnected)

	// Listen for peer connections and register them
	go func() {
		for evt := range peerConnectCh {
			pe := evt.Data.(network.PeerEvent)
			fs.OnPeerConnect(pe.Conn)
		}
	}()

	// Listen for peer disconnections and unregister them
	go func() {
		for evt := range peerDisconnectCh {
			pe := evt.Data.(network.PeerEvent)
			fs.OnPeerDisconnect(pe.Conn)
		}
	}()

	// Attach network manager to control connections
	// Must be after subscriptions to ensure events are received
	network.NewManager(fs.Config.NodeConfig.MaxConnection, fs.logger, node, fs.bus)

	// Start discovery with configured methods
	fs.discoveryMgr = discovery.NewManager(node, fs.bus, fs.Config.NodeConfig.DiscoveryConfig, fs.logger)
	if err := fs.discoveryMgr.Start(); err != nil {
		return err
	}

	fs.logger.Info("Discovery started", observability.Fields{
		"methods": fs.Config.NodeConfig.DiscoveryConfig.EnabledMethods,
	})

	return nil
}

func (fs *FileServer) Stop() {
	if fs.discoveryMgr != nil {
		fs.discoveryMgr.Stop()
	}
	fs.node.Close()
}

func (fs *FileServer) handleMessage(peerID peer.ID, msg core.Message) (core.Message, error) {
	protoMsg, ok := msg.(protocol.Message)
	if !ok {
		return protocol.Message{Type: protocol.TypeError, Data: protocol.DataInvalidMsg}, nil
	}

	switch protoMsg.Type {
	case protocol.TypeStoreFile:
		return fs.handleStoreFileMessage(peerID, protoMsg.Key)
	case protocol.TypeGetFile:
		return fs.handleGetFileMessage(peerID, protoMsg.Key)
	default:
		return protocol.Message{Type: protocol.TypeError, Data: protocol.DataUnknownType}, nil
	}
}

func (fs *FileServer) handleStoreFileMessage(peerID peer.ID, key string) (core.Message, error) {
	fs.logger.Info("store message received", observability.Fields{
		"key": key,
	})

	// Validate the request
	if key == "" {
		return protocol.Message{Type: protocol.TypeStoreFileAck, Data: protocol.DataEmptyKey}, nil
	}

	// Check if we have the peer registered
	_, err := fs.getPeer(peerID.String())
	if err != nil {
		return protocol.Message{Type: protocol.TypeStoreFileAck, Data: protocol.DataPeerNotFound}, nil
	}

	fs.logger.Info("ready to receive file transfer", observability.Fields{
		"peer": peerID.String(),
		"key":  key,
	})

	// Respond with success - the peer should now open a transfer stream
	// The actual file data will come through the /storage-transfer/1.0.0 protocol
	return protocol.Message{Type: protocol.TypeStoreFileAck, Key: key, Data: protocol.DataSuccess}, nil
}

func (fs *FileServer) handleGetFileMessage(peerID peer.ID, key string) (core.Message, error) {
	fs.logger.Info("get message received", observability.Fields{
		"key": key,
	})

	// Validate the request
	if key == "" {
		return protocol.Message{Type: protocol.TypeGetFileResp, Data: protocol.DataEmptyKey}, nil
	}

	// Check if file exists
	if !fs.store.Has(key) {
		fs.logger.Info("file requested via peer not found", observability.Fields{
			"peer": peerID.String(),
			"key":  key,
		})
		return protocol.Message{Type: protocol.TypeGetFileResp, Key: key, Data: protocol.DataNotFound}, nil
	}

	// Read decrypted file from store for network transfer
	// Peer will re-encrypt with their own key when storing
	r, size, err := fs.store.Read(key)
	if err != nil {
		fs.logger.Error("failed to read file from store", observability.Fields{
			"error": err.Error(),
			"key":   key,
		})
		return protocol.Message{Type: protocol.TypeGetFileResp, Key: key, Data: protocol.DataReadFailed}, nil
	}

	// Check if peer is registered
	_, err = fs.getPeer(peerID.String())
	if err != nil {
		return protocol.Message{Type: protocol.TypeGetFileResp, Key: key, Data: protocol.DataPeerNotFound}, nil
	}

	fs.logger.Info("sending file to peer", observability.Fields{
		"peer": peerID.String(),
		"key":  key,
		"size": size,
	})

	// Send file via a new stream in a goroutine
	// The response tells the peer we have the file and will send it
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), TransferTimeout)
		defer cancel()

		if err := fs.transferHandler.SendFile(ctx, peerID, key, r, size); err != nil {
			fs.logger.Error("failed to send file to peer", observability.Fields{
				"error": err.Error(),
				"peer":  peerID.String(),
				"key":   key,
			})
		}
	}()

	return protocol.Message{Type: protocol.TypeGetFileResp, Key: key, Data: protocol.DataFound}, nil
}

// ============================================================================
// Client Methods - For sending requests to peers
// ============================================================================

// ListPeers returns a list of all connected peer IDs
func (fs *FileServer) ListPeers() []peer.ID {
	fs.peerLock.Lock()
	defer fs.peerLock.Unlock()

	peers := make([]peer.ID, 0, len(fs.peers))
	for _, p := range fs.peers {
		peers = append(peers, p.ID)
	}
	return peers
}

// GetConnectedPeers returns the number of connected peers
func (fs *FileServer) GetConnectedPeers() int {
	fs.peerLock.Lock()
	defer fs.peerLock.Unlock()
	return len(fs.peers)
}

// StoreFileToPeer sends a file from local storage to a specific peer
// The file is decrypted before sending so the peer can re-encrypt with their own key
func (fs *FileServer) StoreFileToPeer(ctx context.Context, peerID peer.ID, key string) error {
	// Read decrypted file from local storage
	r, size, err := fs.store.Read(key)
	if err != nil {
		return fmt.Errorf("failed to read file from local storage: %w", err)
	}

	fs.logger.Info("storing file to peer", observability.Fields{
		"peer": peerID.String(),
		"key":  key,
		"size": size,
	})

	// Send the file via transfer protocol
	if err := fs.transferHandler.SendFile(ctx, peerID, key, r, size); err != nil {
		return fmt.Errorf("failed to transfer file: %w", err)
	}

	fs.logger.Info("file stored successfully", observability.Fields{
		"peer": peerID.String(),
		"key":  key,
	})

	return nil
}

// StoreFileToNetwork stores a file to multiple peers in the network
// Reads from local storage using ReadRaw to send encrypted bytes
// Returns the number of successful stores and any errors
func (fs *FileServer) StoreFileToNetwork(ctx context.Context, key string, replicationFactor int) (int, error) {
	peers := fs.ListPeers()
	if len(peers) == 0 {
		return 0, fmt.Errorf("no connected peers")
	}

	// Limit replication factor to available peers
	if replicationFactor > len(peers) {
		replicationFactor = len(peers)
	}

	successCount := 0
	var lastErr error

	for i := 0; i < replicationFactor && i < len(peers); i++ {
		if err := fs.StoreFileToPeer(ctx, peers[i], key); err != nil {
			fs.logger.Error("failed to store file to peer", observability.Fields{
				"peer":  peers[i].String(),
				"key":   key,
				"error": err.Error(),
			})
			lastErr = err
			continue
		}
		successCount++
	}

	if successCount == 0 {
		return 0, fmt.Errorf("failed to store file to any peer: %w", lastErr)
	}

	return successCount, nil
}

// StoreFile stores a file locally first, then propagates to the network
// Returns the number of peers the file was stored to (doesn't count local)
func (fs *FileServer) StoreFile(ctx context.Context, key string, filePath string, replicationFactor int) (int, error) {
	// Store locally first if we don't have it
	if !fs.store.Has(key) {
		file, err := os.Open(filePath)
		if err != nil {
			return 0, fmt.Errorf("failed to open file: %w", err)
		}
		defer file.Close()

		if _, err := fs.store.Write(key, file); err != nil {
			return 0, fmt.Errorf("failed to store file locally: %w", err)
		}

		fs.logger.Info("file stored locally", observability.Fields{
			"key":  key,
			"path": filePath,
		})
	} else {
		fs.logger.Info("file already exists locally", observability.Fields{"key": key})
	}

	// Propagate to network
	peers := fs.ListPeers()
	if len(peers) == 0 {
		fs.logger.Info("no peers available, file stored locally only", observability.Fields{"key": key})
		return 0, nil
	}

	return fs.StoreFileToNetwork(ctx, key, replicationFactor)
}

// GetFileFromPeer requests a file from a specific peer
// The peer will open a transfer stream back to us with the file data
func (fs *FileServer) GetFileFromPeer(ctx context.Context, peerID peer.ID, key string, outputPath string) error {
	fs.logger.Info("requesting file from peer", observability.Fields{
		"peer": peerID.String(),
		"key":  key,
	})

	// Register a waiter before sending the request to avoid race conditions
	waiter := fs.registerTransferWaiter(key)

	// Send get file request via the messaging protocol
	resp, err := fs.proto.Send(ctx, peerID, protocol.Message{
		Type: protocol.TypeGetFile,
		Key:  key,
	})
	if err != nil {
		fs.notifyTransferComplete(key, err) // Clean up waiter
		return fmt.Errorf("failed to send get request: %w", err)
	}

	fs.logger.Info("received get file response", observability.Fields{
		"response": resp,
	})

	// Check if the peer has the file
	if resp.Data == protocol.DataNotFound {
		fs.notifyTransferComplete(key, fmt.Errorf("file not found"))
		return fmt.Errorf("peer does not have the file: %s", key)
	}

	if resp.Data != protocol.DataFound {
		fs.notifyTransferComplete(key, fmt.Errorf("unexpected response: %s", resp.Data))
		return fmt.Errorf("unexpected response from peer: %s", resp.Data)
	}

	// Wait for the transfer to complete with timeout
	select {
	case err := <-waiter:
		if err != nil {
			return fmt.Errorf("file transfer failed: %w", err)
		}
	case <-ctx.Done():
		return fmt.Errorf("context cancelled while waiting for file transfer: %w", ctx.Err())
	}

	// Verify we received the file
	if !fs.store.Has(key) {
		return fmt.Errorf("file transfer completed but file not found in store")
	}

	// If output path is specified, copy from store to output path
	// File was received decrypted and stored encrypted with our key, so use Read
	if outputPath != "" {
		r, _, err := fs.store.Read(key)
		if err != nil {
			return fmt.Errorf("failed to read file from store: %w", err)
		}

		outFile, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer outFile.Close()

		if _, err := io.Copy(outFile, r); err != nil {
			return fmt.Errorf("failed to write output file: %w", err)
		}

		fs.logger.Info("file saved to output path", observability.Fields{
			"key":  key,
			"path": outputPath,
		})
	}

	return nil
}

// GetFileFromNetwork tries to get a file from any peer that has it
func (fs *FileServer) GetFileFromNetwork(ctx context.Context, key string, outputPath string) error {
	peers := fs.ListPeers()
	if len(peers) == 0 {
		return fmt.Errorf("no connected peers")
	}

	// Try each peer until we get the file
	for _, p := range peers {
		if err := fs.GetFileFromPeer(ctx, p, key, outputPath); err != nil {
			fs.logger.Info("peer did not have file, trying next", observability.Fields{
				"peer":  p.String(),
				"key":   key,
				"error": err.Error(),
			})
			continue
		}
		return nil
	}

	return fmt.Errorf("file not found on any peer")
}

// HasFile checks if a file exists locally
func (fs *FileServer) HasFile(key string) bool {
	return fs.store.Has(key)
}

// GetFile gets a file - first checks local storage, then tries network if not found
// If outputPath is specified, saves the file to that path
func (fs *FileServer) GetFile(ctx context.Context, key string, outputPath string) error {
	// Check if file exists locally first
	if fs.store.Has(key) {
		fs.logger.Info("file found locally", observability.Fields{"key": key})

		if outputPath != "" {
			r, _, err := fs.store.Read(key)
			if err != nil {
				// Decryption failed - file may have been received from a peer
				// (encrypted with their key) or stored with a different key
				fs.logger.Error("failed to decrypt local file, may be from peer", observability.Fields{
					"key":   key,
					"error": err.Error(),
				})
				return fmt.Errorf("failed to read local file: %w (file may have been received from a peer and encrypted with their key)", err)
			}

			outFile, err := os.Create(outputPath)
			if err != nil {
				return fmt.Errorf("failed to create output file: %w", err)
			}
			defer outFile.Close()

			if _, err := io.Copy(outFile, r); err != nil {
				return fmt.Errorf("failed to write output file: %w", err)
			}

			fs.logger.Info("local file saved to output path", observability.Fields{
				"key":  key,
				"path": outputPath,
			})
		}
		return nil
	}

	// File not found locally, try network
	fs.logger.Info("file not found locally, trying network", observability.Fields{"key": key})
	return fs.GetFileFromNetwork(ctx, key, outputPath)
}

// GetNodeID returns this node's peer ID
func (fs *FileServer) GetNodeID() peer.ID {
	return fs.node.ID()
}

// GetNodeAddresses returns this node's multiaddrs
func (fs *FileServer) GetNodeAddresses() []string {
	addrs := make([]string, 0, len(fs.node.Addrs()))
	for _, addr := range fs.node.Addrs() {
		addrs = append(addrs, fmt.Sprintf("%s/p2p/%s", addr, fs.node.ID()))
	}
	return addrs
}
