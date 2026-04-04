package operations

import (
	"context"
	"fmt"
	"os"

	"github.com/chiragsoni81245/p2p-storage/internal/config"
	"github.com/chiragsoni81245/p2p-storage/internal/fileserver"
	"github.com/libp2p/go-libp2p/core/peer"
)

// SendFile stores a file locally then sends it directly to the peer at peerAddr.
// It blocks until the transfer completes or ctx is cancelled.
func SendFile(ctx context.Context, fs *fileserver.FileServer, cfg *config.YAMLConfig, filePath, peerAddr string, opts SendOpts) (*SendResult, error) {
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("file does not exist: %s", filePath)
	}
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("cannot send a directory: %s", filePath)
	}

	key, err := GetFileKey(filePath)
	if err != nil {
		return nil, err
	}

	// Store locally first if needed
	if !fs.HasFile(key) {
		storeCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
		_, storeErr := fs.StoreFile(storeCtx, key, filePath, 0)
		cancel()
		if storeErr != nil {
			return nil, fmt.Errorf("failed to store file locally: %w", storeErr)
		}
	}

	connCtx, connCancel := context.WithTimeout(ctx, cfg.Timeout)
	var targetPeerID peer.ID
	targetPeerID, err = fs.ConnectWithHolePunch(connCtx, peerAddr, fileserver.ConnectOpts{
		HolePunchWait: cfg.Node.HolePunchWait,
	})
	connCancel()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to peer: %w", err)
	}

	// Transfer the file
	transferCtx, transferCancel := context.WithTimeout(ctx, cfg.Timeout)
	defer transferCancel()

	err = fs.StoreFileToPeerDirect(transferCtx, targetPeerID, key, opts.Session)
	if err != nil {
		if err == fileserver.ErrRelayedConnection {
			return nil, fmt.Errorf("hole punching failed: no direct connection to peer (only relay available)")
		}
		return nil, fmt.Errorf("failed to send file: %w", err)
	}

	return &SendResult{
		Key:    key,
		PeerID: targetPeerID.String(),
	}, nil
}
