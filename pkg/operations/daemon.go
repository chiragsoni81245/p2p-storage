package operations

import (
	"context"
	"io"

	"github.com/chiragsoni81245/p2p-storage/internal/config"
	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/chiragsoni81245/p2p-storage/internal/fileserver"
)

// RunDaemon starts a propagation node, subscribes to get events
// for caller logging via onEvent, and blocks until ctx is cancelled.
func RunDaemon(ctx context.Context, cfg *config.YAMLConfig, logWriter io.Writer, onEvent func(event.Event)) error {
	fs, err := StartServer(cfg, logWriter)
	if err != nil {
		return err
	}
	defer fs.Stop()

	bus := fs.GetBus()
	receivedCh := bus.Subscribe(event.GetReceived)
	deliveringCh := bus.Subscribe(event.GetDelivering)
	deliveredCh := bus.Subscribe(event.GetDelivered)
	defer bus.Unsubscribe(event.GetReceived, receivedCh)
	defer bus.Unsubscribe(event.GetDelivering, deliveringCh)
	defer bus.Unsubscribe(event.GetDelivered, deliveredCh)

	if onEvent != nil {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case evt, ok := <-receivedCh:
					if ok {
						onEvent(evt)
					}
				case evt, ok := <-deliveringCh:
					if ok {
						onEvent(evt)
					}
				case evt, ok := <-deliveredCh:
					if ok {
						onEvent(evt)
					}
				}
			}
		}()
	}

	<-ctx.Done()
	return nil
}

// StartDaemon is like RunDaemon but uses the FileServer the caller already started.
// Useful when the caller needs the fs reference (e.g. to print node addresses first).
func StartDaemon(ctx context.Context, fs *fileserver.FileServer, onEvent func(event.Event)) {

	bus := fs.GetBus()
	receivedCh := bus.Subscribe(event.GetReceived)
	deliveringCh := bus.Subscribe(event.GetDelivering)
	deliveredCh := bus.Subscribe(event.GetDelivered)

	go func() {
		defer bus.Unsubscribe(event.GetReceived, receivedCh)
		defer bus.Unsubscribe(event.GetDelivering, deliveringCh)
		defer bus.Unsubscribe(event.GetDelivered, deliveredCh)

		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-receivedCh:
				if ok && onEvent != nil {
					onEvent(evt)
				}
			case evt, ok := <-deliveringCh:
				if ok && onEvent != nil {
					onEvent(evt)
				}
			case evt, ok := <-deliveredCh:
				if ok && onEvent != nil {
					onEvent(evt)
				}
			}
		}
	}()
}
