package operations

import (
	"context"
	"fmt"

	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/chiragsoni81245/p2p-storage/internal/fileserver"
	"github.com/chiragsoni81245/p2p-storage/internal/protocol"
	"github.com/google/uuid"
)

// Get broadcasts a GET_FILE request into the network.
// It registers a transfer waiter so the delivery will be accepted without a session.
// Follow with WaitForGet to block until the file arrives.
func Get(ctx context.Context, fs *fileserver.FileServer, key string, bus *event.Bus) {
	// Register waiter before broadcasting so we don't miss the incoming transfer
	fs.RegisterTransferWaiter(key)

	payload := protocol.GetFilePayload{
		Key:            key,
		MsgID:          uuid.New().String(),
		TTL:            fileserver.DefaultTTL,
		RequesterID:    fs.GetNodeID().String(),
		RequesterAddrs: fs.GetNodeAddresses(),
	}

	fs.BroadcastGet(ctx, payload)
}

// WaitForGet blocks until an incoming transfer for key completes (FileReceiveComplete),
// or until ctx expires. Returns the key on success.
func WaitForGet(ctx context.Context, bus *event.Bus, key string) (string, error) {
	completeCh := bus.Subscribe(event.FileReceiveComplete)
	failedCh := bus.Subscribe(event.FileReceiveFailed)
	defer bus.Unsubscribe(event.FileReceiveComplete, completeCh)
	defer bus.Unsubscribe(event.FileReceiveFailed, failedCh)

	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("no peer delivered the file within timeout (%s)", ctx.Err())
		case evt, ok := <-completeCh:
			if !ok {
				continue
			}
			d := evt.Data.(event.ReceiveCompleteData)
			if d.Key != key {
				continue
			}
			return d.Key, nil
		case evt, ok := <-failedCh:
			if !ok {
				continue
			}
			d := evt.Data.(event.ReceiveFailedData)
			if d.Key != key {
				continue
			}
			return "", d.Err
		}
	}
}
