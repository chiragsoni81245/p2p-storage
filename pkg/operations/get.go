package operations

import (
	"context"
	"fmt"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/config"
	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/chiragsoni81245/p2p-storage/internal/fileserver"
)

// GetFile retrieves a file by key and saves it to destPath.
// The operation runs in the background; progress and completion are published
// to bus under the given requestID. The caller should subscribe to
// event.FileGetStarted, event.FileGetComplete, and event.FileGetFailed
// before calling this function.
func GetFile(fs *fileserver.FileServer, cfg *config.YAMLConfig, key, destPath string, requestID RequestID, bus *event.Bus) {
	go func() {
		bus.Publish(event.Event{
			Type:      event.FileGetStarted,
			RequestID: requestID,
			Data:      event.GetStartedData{Key: key},
		})

		ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
		defer cancel()

		if !fs.HasFile(key) {
			peerCount := WaitForPeers(fs, cfg.PeerWait)
			if peerCount == 0 {
				bus.Publish(event.Event{
					Type:      event.FileGetFailed,
					RequestID: requestID,
					Data: event.GetFailedData{
						Key: key,
						Err: fmt.Errorf("no peers available and file not found locally"),
					},
				})
				return
			}

			// Publish initial progress so subscribers know transfer is starting
			bus.Publish(event.Event{
				Type:      event.FileGetProgress,
				RequestID: requestID,
				Data: event.GetProgressData{
					Key:           key,
					BytesReceived: 0,
					TotalBytes:    -1, // unknown until transfer begins
				},
			})
		}

		if err := fs.GetFile(ctx, key, destPath); err != nil {
			bus.Publish(event.Event{
				Type:      event.FileGetFailed,
				RequestID: requestID,
				Data:      event.GetFailedData{Key: key, Err: err},
			})
			return
		}

		bus.Publish(event.Event{
			Type:      event.FileGetComplete,
			RequestID: requestID,
			Data:      event.GetCompleteData{Key: key, SavedPath: destPath},
		})
	}()
}

// WaitForGet blocks until the get operation identified by requestID completes
// or fails, or until ctx is cancelled. Returns the saved path on success.
func WaitForGet(ctx context.Context, bus *event.Bus, requestID RequestID) (string, error) {
	completeCh := bus.Subscribe(event.FileGetComplete)
	failedCh := bus.Subscribe(event.FileGetFailed)
	defer bus.Unsubscribe(event.FileGetComplete, completeCh)
	defer bus.Unsubscribe(event.FileGetFailed, failedCh)

	timeout := time.After(24 * time.Hour) // effectively unbounded; ctx controls cancellation

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timeout:
			return "", fmt.Errorf("timed out waiting for get operation")
		case evt, ok := <-completeCh:
			if !ok {
				continue
			}
			if evt.RequestID != requestID {
				continue
			}
			data := evt.Data.(event.GetCompleteData)
			return data.SavedPath, nil
		case evt, ok := <-failedCh:
			if !ok {
				continue
			}
			if evt.RequestID != requestID {
				continue
			}
			data := evt.Data.(event.GetFailedData)
			return "", data.Err
		}
	}
}
