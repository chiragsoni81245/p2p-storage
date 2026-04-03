package operations

import (
	"context"

	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/chiragsoni81245/p2p-storage/internal/fileserver"
)

// StartReceiveSession enables session-based auth on fs, subscribes to all
// FileReceive* events, calls onEvent for each, and blocks until ctx is done.
func StartReceiveSession(ctx context.Context, fs *fileserver.FileServer, sessionName string, onEvent func(event.Event)) {
	fs.SetSession(sessionName)
	defer fs.ClearSession()

	bus := fs.GetBus()

	startedCh := bus.Subscribe(event.FileReceiveStarted)
	progressCh := bus.Subscribe(event.FileReceiveProgress)
	completeCh := bus.Subscribe(event.FileReceiveComplete)
	failedCh := bus.Subscribe(event.FileReceiveFailed)
	defer bus.Unsubscribe(event.FileReceiveStarted, startedCh)
	defer bus.Unsubscribe(event.FileReceiveProgress, progressCh)
	defer bus.Unsubscribe(event.FileReceiveComplete, completeCh)
	defer bus.Unsubscribe(event.FileReceiveFailed, failedCh)

	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-startedCh:
			onEvent(evt)
		case evt := <-progressCh:
			onEvent(evt)
		case evt := <-completeCh:
			onEvent(evt)
		case evt := <-failedCh:
			onEvent(evt)
		}
	}
}
