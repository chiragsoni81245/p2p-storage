package fileserver

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

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

type Message struct {
	Payload any
}

type MessageGetFile struct {
	Key string
}

type MessageStoreFile struct {
	Key  string
	Size int64
}

// Response message types

type MessageStoreFileAck struct {
	Success bool
	Error   string
}

type MessageGetFileResponse struct {
	Found bool
	Size  int64
	Error string
}

type FileServerOpts struct {
	StorageRoot       string
	PathTransformFunc store.PathTransformFunc
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
}

// StorageHandler implements core.Handler for the storage protocol
type StorageHandler struct {
	fs     *FileServer
	Logger *observability.Logger
}

func (h *StorageHandler) Handle(ctx context.Context, peerID peer.ID, msg core.Message) (core.Message, error) {
	return h.fs.HandleMessage(peerID, msg)
}

func NewFileServer(opts FileServerOpts) *FileServer {
	storeOpts := store.StoreOpts{
		Root:              opts.StorageRoot,
		PathTransformFunc: opts.PathTransformFunc,
	}

	logger := observability.NewLogger(observability.Fields{})
	metrics := observability.NewMetrics()
	bus := event.NewBus()
	scorer := network.NewPeerScorer() // Manage peer score and use best peers

	return &FileServer{
		FileServerOpts: opts,
		store:          store.NewStore(storeOpts),
		quitch:         make(chan struct{}),
		peers:          make(map[string]Peer),
		logger:         logger,
		metrics:        metrics,
		bus:            bus,
		scorer:         scorer,
	}
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

func (fs *FileServer) OnPeer(conn libp2p_network.Conn) {
	fs.peerLock.Lock()
	defer fs.peerLock.Unlock()

	peerID := conn.LocalPeer().String()
	fs.peers[peerID] = Peer{
		ID:   conn.LocalPeer(),
		Conn: conn,
	}
	fs.logger.Info("peer connected", observability.Fields{
		"peer":      peerID,
		"direction": conn.Stat().Direction,
	})
}

func (fs *FileServer) Start() error {
	cfg := node.DefaultConfig()
	node, err := node.NewNode(cfg)
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
	var handler core.Handler = &StorageHandler{
		Logger: fs.logger,
		fs:     fs,
	}

	// Apply Rate Limiting
	rl := middleware.NewRateLimiter(
		10,            // max 10 requests
		time.Second,   // per second
		5*time.Minute, // cleanup inactive peers
	)
	handler = rl.Wrap(handler)

	protocolConfig := protocol.DefaultConfig()
	limiter := middleware.NewLimiter(100) // Max concurrent request 100

	fs.proto = protocol.New(node, "/storage/1.0.0", handler, protocolConfig, fs.logger, limiter)

	// Initialize the file transfer handler for streaming file data
	fs.transferHandler = NewFileTransferHandler(node, fs, fs.logger)

	// Attach any bus event subscribers here first
	go func() {
		for evt := range fs.bus.Subscribe(event.PeerConnected) {
			pe := evt.Data.(network.PeerEvent)
			fs.OnPeer(pe.Conn)
		}
	}()
	//

	// Attach network manager to control connections
	// Must be after subscriptions to ensure events are received
	network.NewManager(cfg, fs.logger, node, fs.bus)

	// Start discovery
	if err := discovery.StartMDNS(node, fs.bus, "storage"); err != nil {
		return err
	}

	fs.logger.Info("Discovery started", observability.Fields{})

	return nil
}

func (fs *FileServer) Stop() {
	fs.node.Close()
}

func (fs *FileServer) HandleMessage(peerID peer.ID, msg core.Message) (core.Message, error) {
	switch payload := msg.(Message).Payload.(type) {
	case *MessageStoreFile:
		return fs.handleStoreFileMessage(peerID, payload)
	case *MessageGetFile:
		return fs.handleGetFileMessage(peerID, payload)
	}
	return nil, nil
}

func (fs *FileServer) handleStoreFileMessage(peerID peer.ID, msgPayload *MessageStoreFile) (core.Message, error) {
	fs.logger.Info("store message received", observability.Fields{
		"key":  msgPayload.Key,
		"size": msgPayload.Size,
	})

	// Validate the request
	if msgPayload.Key == "" {
		return Message{Payload: &MessageStoreFileAck{
			Success: false,
			Error:   "key cannot be empty",
		}}, nil
	}

	if msgPayload.Size <= 0 {
		return Message{Payload: &MessageStoreFileAck{
			Success: false,
			Error:   "invalid file size",
		}}, nil
	}

	// Check if we have the peer registered
	_, err := fs.getPeer(peerID.String())
	if err != nil {
		return Message{Payload: &MessageStoreFileAck{
			Success: false,
			Error:   "peer not registered",
		}}, nil
	}

	fs.logger.Info("ready to receive file transfer", observability.Fields{
		"peer": peerID.String(),
		"key":  msgPayload.Key,
		"size": msgPayload.Size,
	})

	// Respond with success - the peer should now open a transfer stream
	// The actual file data will come through the /storage-transfer/1.0.0 protocol
	return Message{Payload: &MessageStoreFileAck{
		Success: true,
	}}, nil
}

func (fs *FileServer) handleGetFileMessage(peerID peer.ID, msgPayload *MessageGetFile) (core.Message, error) {
	fs.logger.Info("get message received", observability.Fields{
		"key": msgPayload.Key,
	})

	// Validate the request
	if msgPayload.Key == "" {
		return Message{Payload: &MessageGetFileResponse{
			Found: false,
			Error: "key cannot be empty",
		}}, nil
	}

	// Check if file exists
	if !fs.store.Has(msgPayload.Key) {
		fs.logger.Info("file requested via peer not found", observability.Fields{
			"peer": peerID.String(),
			"key":  msgPayload.Key,
		})
		return Message{Payload: &MessageGetFileResponse{
			Found: false,
			Error: "file not found",
		}}, nil
	}

	// Read file from store
	r, size, err := fs.store.Read(msgPayload.Key)
	if err != nil {
		fs.logger.Error("failed to read file from store", observability.Fields{
			"error": err.Error(),
			"key":   msgPayload.Key,
		})
		return Message{Payload: &MessageGetFileResponse{
			Found: false,
			Error: "failed to read file",
		}}, nil
	}

	// Check if peer is registered
	_, err = fs.getPeer(peerID.String())
	if err != nil {
		return Message{Payload: &MessageGetFileResponse{
			Found: false,
			Error: "peer not registered",
		}}, nil
	}

	fs.logger.Info("sending file to peer", observability.Fields{
		"peer": peerID.String(),
		"key":  msgPayload.Key,
		"size": size,
	})

	// Send file via a new stream in a goroutine
	// The response tells the peer we have the file and will send it
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), TransferTimeout)
		defer cancel()

		if err := fs.transferHandler.SendFile(ctx, peerID, msgPayload.Key, r, size); err != nil {
			fs.logger.Error("failed to send file to peer", observability.Fields{
				"error": err.Error(),
				"peer":  peerID.String(),
				"key":   msgPayload.Key,
			})
		}
	}()

	return Message{Payload: &MessageGetFileResponse{
		Found: true,
		Size:  size,
	}}, nil
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

// StoreFileToPeer sends a file to a specific peer for storage
// It first sends a store request, then opens a transfer stream to send the file data
func (fs *FileServer) StoreFileToPeer(ctx context.Context, peerID peer.ID, key string, filePath string) error {
	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Get file size
	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}
	size := stat.Size()

	fs.logger.Info("storing file to peer", observability.Fields{
		"peer": peerID.String(),
		"key":  key,
		"size": size,
		"path": filePath,
	})

	// Send the file via transfer protocol
	if err := fs.transferHandler.SendFile(ctx, peerID, key, file, size); err != nil {
		return fmt.Errorf("failed to transfer file: %w", err)
	}

	fs.logger.Info("file stored successfully", observability.Fields{
		"peer": peerID.String(),
		"key":  key,
	})

	return nil
}

// StoreFileToNetwork stores a file to multiple peers in the network
// Returns the number of successful stores and any errors
func (fs *FileServer) StoreFileToNetwork(ctx context.Context, key string, filePath string, replicationFactor int) (int, error) {
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
		if err := fs.StoreFileToPeer(ctx, peers[i], key, filePath); err != nil {
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

// GetFileFromPeer requests a file from a specific peer
// The peer will open a transfer stream back to us with the file data
func (fs *FileServer) GetFileFromPeer(ctx context.Context, peerID peer.ID, key string, outputPath string) error {
	fs.logger.Info("requesting file from peer", observability.Fields{
		"peer": peerID.String(),
		"key":  key,
	})

	// Send get file request via the messaging protocol
	resp, err := fs.proto.Send(ctx, peerID, protocol.Message{
		Type: "GET_FILE",
		Key:  key,
	})
	if err != nil {
		return fmt.Errorf("failed to send get request: %w", err)
	}

	fs.logger.Info("received get file response", observability.Fields{
		"response": resp,
	})

	// The peer will send the file via the transfer protocol
	// We need to wait for it to arrive and be stored locally
	// For now, wait a bit for the transfer to complete, then copy from local store
	time.Sleep(2 * time.Second)

	// Check if we received the file
	if !fs.store.Has(key) {
		return fmt.Errorf("file transfer did not complete")
	}

	// If output path is specified, copy from store to output path
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
