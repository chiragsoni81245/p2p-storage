package fileserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

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
	ma "github.com/multiformats/go-multiaddr"
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

	// Get (hop) state
	seen     *SeenMessages
	selector PeerSelector

	// Session for receive mode
	sessionLock sync.RWMutex
	sessionName string

	// Transfer completion waiters
	waitersLock sync.RWMutex
	waiters     map[string][]chan error
}

// Handle implements core.Handler for the storage protocol
func (fs *FileServer) Handle(ctx context.Context, peerID peer.ID, msg core.Message) (core.Message, error) {
	return fs.handleMessage(peerID, msg)
}

func NewFileServer(opts FileServerOpts) (*FileServer, error) {
	// Use provided log writer or default to stdout
	logWriter := opts.LogWriter
	if logWriter == nil {
		logWriter = os.Stdout
	}
	logLevel := observability.ParseLogLevel(opts.LogLevel)
	logger := observability.NewLoggerWithLevel(logWriter, observability.Fields{}, logLevel)

	// Default encryption to enabled
	encryption := store.EncryptionConfig{Enabled: true}
	if opts.Encryption != nil {
		encryption = *opts.Encryption
	}

	storeOpts := store.StoreOpts{
		Root:              opts.StorageRoot,
		PathTransformFunc: opts.PathTransformFunc,
		Encryption:        encryption,
		Logger: logger,
	}


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
		seen:           NewSeenMessages(10000, 60*time.Second),
		selector:       &RandomPeerSelector{},
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

// RegisterTransferWaiter registers a channel to be notified when a file transfer completes.
// It is exported so operations.Get can pre-register before broadcasting.
func (fs *FileServer) RegisterTransferWaiter(key string) chan error {
	return fs.registerTransferWaiter(key)
}

// registerTransferWaiter is the internal implementation.
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

// StorageProtocolID is the protocol identifier for storage operations
const StorageProtocolID = "/storage/1.0.0"

func (fs *FileServer) OnPeerConnect(conn libp2p_network.Conn) {
	peerID := conn.RemotePeer()

	// Wait for connection to stabilize before checking protocol support
	// This avoids checking during connection negotiation/upgrade
	go func() {
		time.Sleep(500 * time.Millisecond)

		// Verify connection is still established after stabilization period.
		// Accept Limited (transient/relay) connections as well as full direct ones.
		if fs.node == nil {
			return
		}
		c := fs.node.Network().Connectedness(peerID)
		if c != libp2p_network.Connected && c != libp2p_network.Limited {
			return
		}

		// Check if peer supports our storage protocol
		// Don't disconnect non-storage peers - they're useful for DHT routing and relay
		protocols, err := fs.node.Peerstore().GetProtocols(peerID)
		if err != nil {
			fs.logger.Debug("failed to get peer protocols", observability.Fields{
				"peer_id": peerID.String(),
				"error":   err.Error(),
			})
			return
		}

		supportsStorage := false
		for _, p := range protocols {
			if string(p) == StorageProtocolID {
				supportsStorage = true
				break
			}
		}

		if !supportsStorage {
			fs.logger.Debug("peer doesn't support storage protocol, not adding to storage peers", observability.Fields{
				"peer_id":   peerID.String(),
				"protocols": protocols,
			})
			// Don't disconnect - peer is still useful for DHT/relay
			return
		}

		fs.peerLock.Lock()
		defer fs.peerLock.Unlock()

		// Only add if not already tracked
		if _, exists := fs.peers[peerID.String()]; exists {
			return
		}

		fs.peers[peerID.String()] = Peer{
			ID:   peerID,
			Conn: conn,
		}

		// Protect storage peers from ConnManager trimming by tagging them
		// Higher value = more important, less likely to be trimmed
		fs.node.ConnManager().TagPeer(peerID, "storage-peer", 100)

		// Notify network manager that this is a storage-capable peer
		fs.bus.Publish(event.Event{
			Type: event.StoragePeerRegistered,
			Data: network.StoragePeerEvent{PeerID: peerID},
		})

		fs.logger.Info("storage peer registered", observability.Fields{
			"peer_id":    peerID.String(),
			"peer_count": len(fs.peers),
		})
	}()
}

func (fs *FileServer) OnPeerDisconnect(conn libp2p_network.Conn) {
	peerID := conn.RemotePeer()

	fs.peerLock.Lock()
	defer fs.peerLock.Unlock()

	// Only remove if truly disconnected (no remaining connections, direct or relay).
	if fs.node != nil {
		c := fs.node.Network().Connectedness(peerID)
		if c == libp2p_network.Connected || c == libp2p_network.Limited {
			return
		}
	}

	if _, exists := fs.peers[peerID.String()]; !exists {
		return
	}

	delete(fs.peers, peerID.String())

	// Remove the protection tag since peer is no longer a storage peer
	fs.node.ConnManager().UntagPeer(peerID, "storage-peer")

	// Notify network manager that this storage peer is gone
	fs.bus.Publish(event.Event{
		Type: event.StoragePeerUnregistered,
		Data: network.StoragePeerEvent{PeerID: peerID},
	})

	fs.logger.Info("storage peer unregistered", observability.Fields{
		"peer_id":    peerID.String(),
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

	fs.proto = protocol.New(node, StorageProtocolID, handler, fs.Config.ProtocolConfig, fs.logger, limiter)

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
	// Connection limits are handled by libp2p's ResourceManager (configured in node.go)
	network.NewManager(
		fs.logger,
		node,
		fs.bus,
	)

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
		return protocol.NewMessage(protocol.TypeError, protocol.ErrorPayload{Reason: protocol.StatusInvalidMsg}), nil
	}

	switch protoMsg.Type {
	case protocol.TypeStoreFile:
		return fs.handleStoreFileMessage(peerID, protoMsg)
	case protocol.TypeGetFile:
		fs.handleGet(peerID, protoMsg)
		return nil, nil // fire-and-forget: handleStream skips writing response
	default:
		return protocol.NewMessage(protocol.TypeError, protocol.ErrorPayload{Reason: protocol.StatusUnknownType}), nil
	}
}

func (fs *FileServer) handleStoreFileMessage(peerID peer.ID, msg protocol.Message) (core.Message, error) {
	payload, err := protocol.Decode[protocol.StoreFilePayload](msg)
	if err != nil {
		return protocol.NewMessage(protocol.TypeError, protocol.ErrorPayload{Reason: protocol.StatusInvalidMsg}), nil
	}
	key := payload.Key
	session := payload.Session

	fs.logger.Info("store message received", observability.Fields{
		"key": key,
	})

	// Validate the request
	if key == "" {
		return protocol.NewMessage(protocol.TypeStoreFileResp, protocol.StoreFileRespPayload{Status: protocol.StatusEmptyKey}), nil
	}

	// Check if we are waiting for this file or we have an active receive session which match with user request
	fs.waitersLock.RLock()
	_, waitingForKey := fs.waiters[key]
	fs.waitersLock.RUnlock()

	if !waitingForKey {
		// Validate session if one is active
		fs.sessionLock.RLock()
		activeSession := fs.sessionName
		fs.sessionLock.RUnlock()

		if activeSession != "" && session != activeSession {
			fs.logger.Info("store request rejected: wrong session", observability.Fields{
				"peer": peerID.String(),
				"key":  key,
			})
			return protocol.NewMessage(protocol.TypeStoreFileResp, protocol.StoreFileRespPayload{Key: key, Status: protocol.StatusSessionRejected}), nil
		}
	}

	// Reject if we already have this file locally
	if fs.store.Has(key) {
		fs.logger.Info("store request rejected: file already exists", observability.Fields{
			"peer": peerID.String(),
			"key":  key,
		})
		return protocol.NewMessage(protocol.TypeStoreFileResp, protocol.StoreFileRespPayload{Key: key, Status: protocol.StatusAlreadyExists}), nil
	}

	fs.logger.Info("ready to receive file transfer", observability.Fields{
		"peer": peerID.String(),
		"key":  key,
	})

	return protocol.NewMessage(protocol.TypeStoreFileResp, protocol.StoreFileRespPayload{Key: key, Status: protocol.StatusSuccess}), nil
}

func (fs *FileServer) handleGet(peerID peer.ID, msg protocol.Message) {
	payload, err := protocol.Decode[protocol.GetFilePayload](msg)
	if err != nil {
		fs.logger.Error("failed to decode GET_FILE payload", observability.Fields{
			"error": err.Error(),
		})
		return
	}

	go fs.processGet(peerID, payload)
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

// StoreFileToPeer sends a file from local storage to a specific peer.
// It first negotiates via the control protocol (TypeStoreFile) so the receiver
// can validate the session before the transfer stream is opened.
func (fs *FileServer) StoreFileToPeer(ctx context.Context, peerID peer.ID, key, session string) error {
	// Negotiate with the receiver before opening the transfer stream
	resp, err := fs.proto.Send(ctx, peerID, protocol.NewMessage(protocol.TypeStoreFile, protocol.StoreFilePayload{
		Key:     key,
		Session: session,
	}))
	if err != nil {
		return fmt.Errorf("failed to negotiate file transfer: %w", err)
	}

	ack, err := protocol.Decode[protocol.StoreFileRespPayload](resp)
	if err != nil {
		return fmt.Errorf("failed to decode store response: %w", err)
	}

	switch ack.Status {
	case protocol.StatusSessionRejected:
		return ErrSessionRejected
	case protocol.StatusAlreadyExists:
		return ErrFileAlreadyExists
	case protocol.StatusSuccess:
		// ok
	default:
		return fmt.Errorf("peer rejected transfer: %s", ack.Status)
	}

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

	if err := fs.transferHandler.SendFile(ctx, peerID, key, r, size); err != nil {
		return fmt.Errorf("failed to transfer file: %w", err)
	}

	fs.logger.Info("file stored successfully", observability.Fields{
		"peer": peerID.String(),
		"key":  key,
	})

	return nil
}

// StoreFileToNetwork stores a file to multiple peers in the network.
// Returns the number of successful stores and any errors.
func (fs *FileServer) StoreFileToNetwork(ctx context.Context, key string, replicationFactor int) (int, error) {
	peers := fs.ListPeers()
	if len(peers) == 0 {
		return 0, fmt.Errorf("no connected peers")
	}

	if replicationFactor > len(peers) {
		replicationFactor = len(peers)
	}

	successCount := 0
	var lastErr error

	for i := 0; i < replicationFactor && i < len(peers); i++ {
		if err := fs.StoreFileToPeer(ctx, peers[i], key, ""); err != nil {
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

// HasFile checks if a file exists locally
func (fs *FileServer) HasFile(key string) bool {
	return fs.store.Has(key)
}

// BroadcastGet sends a GET_FILE message to all connected peers.
func (fs *FileServer) BroadcastGet(ctx context.Context, payload protocol.GetFilePayload) {
	msg := protocol.NewMessage(protocol.TypeGetFile, payload)
	for _, p := range fs.ListPeers() {
		p := p
		go func() {
			if err := fs.proto.SendOneWay(ctx, p, msg); err != nil {
				fs.logger.Debug("failed to send GET_FILE to peer", observability.Fields{
					"peer":  p.String(),
					"key":   payload.Key,
					"error": err.Error(),
				})
			}
		}()
	}
}

// WriteFileTo copies a locally stored file to destPath.
func (fs *FileServer) WriteFileTo(key, destPath string) error {
	r, _, err := fs.store.Read(key)
	if err != nil {
		return fmt.Errorf("failed to read file from store: %w", err)
	}

	outFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	if _, err := io.Copy(outFile, r); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	fs.logger.Info("file written to destination", observability.Fields{
		"key":  key,
		"path": destPath,
	})
	return nil
}

// SetSession enables session-based auth for incoming transfers.
// Only senders providing this session name will be accepted.
func (fs *FileServer) SetSession(name string) {
	fs.sessionLock.Lock()
	defer fs.sessionLock.Unlock()
	fs.sessionName = name
}

// ClearSession disables session validation, accepting all incoming transfers.
func (fs *FileServer) ClearSession() {
	fs.sessionLock.Lock()
	defer fs.sessionLock.Unlock()
	fs.sessionName = ""
}

// GetBus returns the internal event bus so callers can subscribe to events.
func (fs *FileServer) GetBus() *event.Bus {
	return fs.bus
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

// ErrRelayedConnection is returned when only relayed connections exist to a peer
var ErrRelayedConnection = errors.New("only relayed connection available, direct connection required")

// ErrSessionRejected is returned when the receiver rejects the session name
var ErrSessionRejected = errors.New("receiver rejected the session name")

// ErrFileAlreadyExists is returned when the receiver already has the file
var ErrFileAlreadyExists = errors.New("receiver already has this file")

// ConnectToPeer connects to a peer using their multiaddr string
// Publishes a PeerDiscovered event so the network manager handles the connection
// Returns the peer ID on success
func (fs *FileServer) ConnectToPeer(ctx context.Context, multiaddr string) (peer.ID, error) {
	maddr, err := ma.NewMultiaddr(multiaddr)
	if err != nil {
		return "", fmt.Errorf("invalid multiaddr: %w", err)
	}

	peerInfo, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return "", fmt.Errorf("failed to parse peer info from multiaddr: %w", err)
	}

	// Publish discovery event - network manager will handle the connection
	fs.bus.Publish(event.Event{
		Type: event.PeerDiscovered,
		Data: discovery.PeerDiscoveredEvent{
			AddrInfo: *peerInfo,
		},
	})

	// Wait for connection to be established
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("timeout waiting for connection: %w", ctx.Err())
		case <-ticker.C:
			c := fs.node.Network().Connectedness(peerInfo.ID)
			// network.Limited = transient/relay connection (go-libp2p v0.29+)
			if c == libp2p_network.Connected || c == libp2p_network.Limited {
				fs.logger.Info("connected to peer", observability.Fields{
					"peer": peerInfo.ID.String(),
				})
				return peerInfo.ID, nil
			}
		}
	}
}

// IsDirectConnection checks if we have a direct (non-relayed) connection to a peer
// Returns true if at least one direct connection exists
func (fs *FileServer) IsDirectConnection(peerID peer.ID) bool {
	conns := fs.node.Network().ConnsToPeer(peerID)
	for _, conn := range conns {
		addr := conn.RemoteMultiaddr().String()
		// Relayed connections contain "/p2p-circuit" in the multiaddr
		if !strings.Contains(addr, "/p2p-circuit") {
			return true
		}
	}
	return false
}

// GetConnectionType returns "direct" or "relayed" based on the connection type
func (fs *FileServer) GetConnectionType(peerID peer.ID) string {
	if fs.IsDirectConnection(peerID) {
		return "direct"
	}
	return "relayed"
}

// WaitForDirectConnection waits for hole punching to establish a direct connection
// Returns nil if direct connection is established, error if timeout or only relayed
func (fs *FileServer) WaitForDirectConnection(ctx context.Context, peerID peer.ID, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	logTicker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	defer logTicker.Stop()

	fs.logger.Info("waiting for hole punch to establish direct connection", observability.Fields{
		"peer":    peerID.String(),
		"timeout": timeout.String(),
	})

	for time.Now().Before(deadline) {
		if fs.IsDirectConnection(peerID) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-logTicker.C:
			conns := fs.node.Network().ConnsToPeer(peerID)
			addrs := make([]string, 0, len(conns))
			for _, c := range conns {
				addrs = append(addrs, c.RemoteMultiaddr().String())
			}
			fs.logger.Info("still waiting for direct connection, current connections", observability.Fields{
				"peer":  peerID.String(),
				"addrs": addrs,
			})
		case <-ticker.C:
			// Check again
		}
	}

	if fs.IsDirectConnection(peerID) {
		return nil
	}
	return ErrRelayedConnection
}

// WaitForRelayReservation blocks until at least one relay circuit address appears
// in the node's address list, indicating a relay reservation is active.
// Returns immediately if no relay servers are configured.
func (fs *FileServer) WaitForRelayReservation(ctx context.Context) error {
	if len(fs.Config.NodeConfig.RelayServers) == 0 {
		return nil
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			for _, addr := range fs.GetNodeAddresses() {
				if strings.Contains(addr, "/p2p-circuit") {
					return nil
				}
			}
		}
	}
}

// StoreFileToPeerDirect sends a file to a peer only if direct connection exists.
// Returns ErrRelayedConnection if only relayed connection is available.
func (fs *FileServer) StoreFileToPeerDirect(ctx context.Context, peerID peer.ID, key, session string) error {
	if !fs.IsDirectConnection(peerID) {
		return ErrRelayedConnection
	}
	return fs.StoreFileToPeer(ctx, peerID, key, session)
}

// ConnectOpts controls how ConnectWithHolePunch behaves.
type ConnectOpts struct {
	// HolePunchWait is how long to wait for hole punching to produce a direct
	// connection after connecting through a relay. Zero skips hole punching.
	HolePunchWait time.Duration
}

// ConnectWithHolePunch connects to peerAddr, transparently handling relay circuit
// addresses. For relay addresses it:
//  1. Connects to the relay
//  2. Waits up to GetConnectTimeout for our own relay reservation to appear
//  3. Connects to the peer through the relay
//  4. If opts.HolePunchWait > 0, waits for hole punching to establish a direct connection
//  5. If !opts.AllowRelay and no direct connection, returns ErrRelayedConnection
//
// Each connection step is bounded by GetConnectTimeout; the outer ctx provides
// an additional cancellation boundary.
func (fs *FileServer) ConnectWithHolePunch(ctx context.Context, peerAddr string, opts ConnectOpts) (peer.ID, error) {
	if !strings.Contains(peerAddr, "/p2p-circuit") {
		return fs.ConnectToPeer(ctx, peerAddr)
	}

	// --- Relay circuit address ---
	relayPart := strings.Split(peerAddr, "/p2p-circuit")[0]

	// 1. Connect to the relay
	relayCtx, relayCancel := context.WithTimeout(ctx, GetConnectTimeout)
	relayPeerID, err := fs.ConnectToPeer(relayCtx, relayPart)
	relayCancel()
	if err != nil {
		return "", fmt.Errorf("failed to connect to relay: %w", err)
	}

	// 2. Wait for our own relay reservation on that relay
	reservationCtx, reservationCancel := context.WithTimeout(ctx, GetConnectTimeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	reservationOK := false
reservationLoop:
	for {
		select {
		case <-reservationCtx.Done():
			break reservationLoop
		case <-ticker.C:
			for _, addr := range fs.GetNodeAddresses() {
				if strings.Contains(addr, relayPeerID.String()) {
					reservationOK = true
					break reservationLoop
				}
			}
		}
	}
	ticker.Stop()
	reservationCancel()
	if !reservationOK {
		return "", fmt.Errorf("timed out waiting for relay reservation on %s", relayPart)
	}

	// 3. Connect to the peer through the relay
	peerCtx, peerCancel := context.WithTimeout(ctx, GetConnectTimeout)
	peerID, err := fs.ConnectToPeer(peerCtx, peerAddr)
	peerCancel()
	if err != nil {
		return "", fmt.Errorf("failed to connect to peer through relay: %w", err)
	}

	// 4. Wait for hole punching to produce a direct connection.
	// DCUtR fires automatically in the background once the relay connection is up;
	// we just poll until a direct connection appears or the wait expires.
	if opts.HolePunchWait > 0 {
		hpCtx, hpCancel := context.WithTimeout(ctx, opts.HolePunchWait)
		err = fs.WaitForDirectConnection(hpCtx, peerID, opts.HolePunchWait)
		hpCancel()
		if err != nil {
			return "", fmt.Errorf("hole punching failed after %s: %w", opts.HolePunchWait, ErrRelayedConnection)
		}
		fs.logger.Info("hole punching succeeded, direct connection established", observability.Fields{
			"peer": peerID.String(),
		})
	}

	return peerID, nil
}
