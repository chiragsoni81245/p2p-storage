package operations

import (
	"context"
	"fmt"
	"os"

	"github.com/chiragsoni81245/p2p-storage/internal/fileserver"
)

// StoreLocally stores a file in the local store only. No peers are contacted.
func StoreLocally(ctx context.Context, fs *fileserver.FileServer, filePath string) (*StoreResult, error) {
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

	if _, err := fs.StoreFile(ctx, key, filePath, 0); err != nil {
		return nil, fmt.Errorf("failed to store file: %w", err)
	}

	return &StoreResult{Key: key}, nil
}
