package fileserver

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/observability"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

const (
	FileTransferProtocol = "/storage-transfer/1.0.0"
	TransferTimeout      = 5 * time.Minute
	MaxKeyLength         = 1024
)

// TransferType indicates the direction of file transfer
type TransferType uint8

const (
	TransferTypeUpload   TransferType = 1 // Peer is uploading a file to us
	TransferTypeDownload TransferType = 2 // We are sending a file to peer
)

// PendingTransfer tracks a file transfer that's been negotiated but not yet started
type PendingTransfer struct {
	Key        string
	Size       int64
	Type       TransferType
	PeerID     peer.ID
	CreatedAt  time.Time
	TransferID string
}

// FileTransferHandler handles the raw file transfer streams
type FileTransferHandler struct {
	host   host.Host
	fs     *FileServer
	logger *observability.Logger

	pendingLock sync.RWMutex
	pending     map[string]*PendingTransfer // transferID -> PendingTransfer
}

// NewFileTransferHandler creates a new file transfer handler
func NewFileTransferHandler(h host.Host, fs *FileServer, logger *observability.Logger) *FileTransferHandler {
	fth := &FileTransferHandler{
		host:    h,
		fs:      fs,
		logger:  logger,
		pending: make(map[string]*PendingTransfer),
	}

	// Register the stream handler for incoming file transfers
	h.SetStreamHandler(protocol.ID(FileTransferProtocol), fth.handleIncomingStream)

	// Start cleanup goroutine for stale pending transfers
	go fth.cleanupStaleTransfers()

	return fth
}

// RegisterPendingTransfer registers a new pending file transfer
func (fth *FileTransferHandler) RegisterPendingTransfer(transferID string, pt *PendingTransfer) {
	fth.pendingLock.Lock()
	defer fth.pendingLock.Unlock()
	fth.pending[transferID] = pt
}

// GetPendingTransfer retrieves and removes a pending transfer
func (fth *FileTransferHandler) GetPendingTransfer(transferID string) (*PendingTransfer, bool) {
	fth.pendingLock.Lock()
	defer fth.pendingLock.Unlock()
	pt, ok := fth.pending[transferID]
	if ok {
		delete(fth.pending, transferID)
	}
	return pt, ok
}

// cleanupStaleTransfers removes transfers that have exceeded the timeout
func (fth *FileTransferHandler) cleanupStaleTransfers() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		fth.pendingLock.Lock()
		now := time.Now()
		for id, pt := range fth.pending {
			if now.Sub(pt.CreatedAt) > TransferTimeout {
				fth.logger.Info("cleaning up stale transfer", observability.Fields{
					"transfer_id": id,
					"key":         pt.Key,
				})
				delete(fth.pending, id)
			}
		}
		fth.pendingLock.Unlock()
	}
}

// handleIncomingStream handles incoming file transfer streams
// Protocol: [1 byte: key length][key bytes][file data...]
func (fth *FileTransferHandler) handleIncomingStream(s network.Stream) {
	defer s.Close()

	remotePeer := s.Conn().RemotePeer()

	// Set read deadline for the header
	_ = s.SetReadDeadline(time.Now().Add(30 * time.Second))

	// Read key length (2 bytes, big endian)
	keyLenBuf := make([]byte, 2)
	if _, err := io.ReadFull(s, keyLenBuf); err != nil {
		fth.logger.Error("failed to read key length", observability.Fields{
			"error": err.Error(),
			"peer":  remotePeer.String(),
		})
		return
	}
	keyLen := binary.BigEndian.Uint16(keyLenBuf)

	if keyLen == 0 || keyLen > MaxKeyLength {
		fth.logger.Error("invalid key length", observability.Fields{
			"key_length": keyLen,
			"peer":       remotePeer.String(),
		})
		return
	}

	// Read key
	keyBuf := make([]byte, keyLen)
	if _, err := io.ReadFull(s, keyBuf); err != nil {
		fth.logger.Error("failed to read key", observability.Fields{
			"error": err.Error(),
			"peer":  remotePeer.String(),
		})
		return
	}
	key := string(keyBuf)

	// Read file size (8 bytes, big endian)
	sizeBuf := make([]byte, 8)
	if _, err := io.ReadFull(s, sizeBuf); err != nil {
		fth.logger.Error("failed to read file size", observability.Fields{
			"error": err.Error(),
			"peer":  remotePeer.String(),
			"key":   key,
		})
		return
	}
	fileSize := int64(binary.BigEndian.Uint64(sizeBuf))

	fth.logger.Info("receiving file transfer", observability.Fields{
		"key":  key,
		"size": fileSize,
		"peer": remotePeer.String(),
	})

	// Reset deadline for file transfer (longer timeout)
	_ = s.SetReadDeadline(time.Now().Add(TransferTimeout))

	// Create a limited reader to avoid reading more than expected
	limitedReader := io.LimitReader(s, fileSize)

	// Write file to store
	written, err := fth.fs.store.Write(key, limitedReader)
	if err != nil {
		fth.logger.Error("failed to write file to store", observability.Fields{
			"error": err.Error(),
			"key":   key,
			"peer":  remotePeer.String(),
		})
		// Send failure response
		s.Write([]byte{0})
		return
	}

	fth.logger.Info("file transfer complete", observability.Fields{
		"key":  key,
		"size": written,
		"peer": remotePeer.String(),
	})

	// Send success response
	s.Write([]byte{1})
}

// SendFile opens a new stream to the peer and sends the file
// Protocol: [2 bytes: key length][key bytes][8 bytes: file size][file data...]
func (fth *FileTransferHandler) SendFile(ctx context.Context, peerID peer.ID, key string, r io.Reader, size int64) error {
	// Open new stream to peer
	stream, err := fth.host.NewStream(ctx, peerID, protocol.ID(FileTransferProtocol))
	if err != nil {
		return fmt.Errorf("failed to open stream: %w", err)
	}
	defer stream.Close()

	// Set write deadline
	_ = stream.SetWriteDeadline(time.Now().Add(TransferTimeout))

	// Write key length (2 bytes, big endian)
	keyBytes := []byte(key)
	keyLenBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(keyLenBuf, uint16(len(keyBytes)))
	if _, err := stream.Write(keyLenBuf); err != nil {
		return fmt.Errorf("failed to write key length: %w", err)
	}

	// Write key
	if _, err := stream.Write(keyBytes); err != nil {
		return fmt.Errorf("failed to write key: %w", err)
	}

	// Write file size (8 bytes, big endian)
	sizeBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(sizeBuf, uint64(size))
	if _, err := stream.Write(sizeBuf); err != nil {
		return fmt.Errorf("failed to write file size: %w", err)
	}

	fth.logger.Info("sending file to peer", observability.Fields{
		"key":  key,
		"size": size,
		"peer": peerID.String(),
	})

	// Stream file data
	written, err := io.Copy(stream, r)
	if err != nil {
		return fmt.Errorf("failed to send file data: %w", err)
	}

	fth.logger.Info("file sent successfully", observability.Fields{
		"key":  key,
		"size": written,
		"peer": peerID.String(),
	})

	// Read response (1 byte: success/failure)
	_ = stream.SetReadDeadline(time.Now().Add(30 * time.Second))
	respBuf := make([]byte, 1)
	if _, err := io.ReadFull(stream, respBuf); err != nil {
		return fmt.Errorf("failed to read transfer response: %w", err)
	}

	if respBuf[0] != 1 {
		return fmt.Errorf("peer reported transfer failure")
	}

	return nil
}
