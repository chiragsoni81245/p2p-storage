package operations

import (
	"context"
	"fmt"
	"os"

	"github.com/chiragsoni81245/p2p-storage/internal/config"
	"github.com/chiragsoni81245/p2p-storage/internal/fileserver"
)

// StoreFile stores a file locally and replicates it to connected peers.
// It blocks until the operation completes or ctx is cancelled.
func StoreFile(ctx context.Context, fs *fileserver.FileServer, cfg *config.YAMLConfig, filePath string) (*StoreResult, error) {
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("file does not exist: %s", filePath)
	}
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("cannot store a directory: %s", filePath)
	}

	key, err := GetFileKey(filePath)
	if err != nil {
		return nil, err
	}

	peerCount := WaitForPeers(fs, cfg.PeerWait)

	replicated, err := fs.StoreFile(ctx, key, filePath, peerCount)
	if err != nil {
		return nil, fmt.Errorf("failed to store file: %w", err)
	}

	return &StoreResult{
		Key:          key,
		ReplicatedTo: replicated,
	}, nil
}
