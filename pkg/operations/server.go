package operations

import (
	"fmt"
	"io"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/config"
	"github.com/chiragsoni81245/p2p-storage/internal/fileserver"
	"github.com/chiragsoni81245/p2p-storage/internal/store"
)

// StartServer creates and starts a FileServer using the provided config and log writer.
// The caller is responsible for calling fs.Stop() when done.
func StartServer(cfg *config.YAMLConfig, logWriter io.Writer) (*fileserver.FileServer, error) {
	fs, err := fileserver.NewFileServer(fileserver.FileServerOpts{
		StorageRoot:       cfg.StorageRoot,
		PathTransformFunc: store.CASPathTransformFunc,
		LogWriter:         logWriter,
		LogLevel:          cfg.LogLevel,
		Config:            cfg.ToConfig(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create file server: %w", err)
	}

	if err := fs.Start(); err != nil {
		return nil, fmt.Errorf("failed to start node: %w", err)
	}

	return fs, nil
}

// WaitForPeers polls until at least one storage peer is connected or the wait
// duration elapses. Returns the number of connected peers (may be 0).
func WaitForPeers(fs *fileserver.FileServer, wait time.Duration) int {
	deadline := time.Now().Add(wait)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		<-ticker.C
		if count := fs.GetConnectedPeers(); count > 0 {
			return count
		}
	}

	return fs.GetConnectedPeers()
}
