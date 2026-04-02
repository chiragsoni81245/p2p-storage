package event

import (
	"sync"
)

type EventType string

const (
	PeerDiscovered          EventType = "peer:discovered"
	PeerConnected           EventType = "peer:connected"
	PeerDisconnected        EventType = "peer:disconnected"
	StoragePeerRegistered   EventType = "peer:storage:registered"   // Peer supports storage protocol
	StoragePeerUnregistered EventType = "peer:storage:unregistered" // Storage peer disconnected
	FileTransferComplete    EventType = "file:transfer:complete"

	// Get operation events
	FileGetStarted  EventType = "file:get:started"
	FileGetProgress EventType = "file:get:progress"
	FileGetComplete EventType = "file:get:complete"
	FileGetFailed   EventType = "file:get:failed"
)

type Event struct {
	Type      EventType
	RequestID string
	Data      any
}

// GetStartedData is published when a get operation begins
type GetStartedData struct {
	Key string
}

// GetProgressData is published periodically during a get operation
type GetProgressData struct {
	Key           string
	BytesReceived int64
	TotalBytes    int64
}

// GetCompleteData is published when a file is successfully retrieved into local storage
type GetCompleteData struct {
	Key string
}

// GetFailedData is published when a get operation fails
type GetFailedData struct {
	Key string
	Err error
}

type Bus struct {
	subscribers map[EventType][]chan Event
	mu          sync.RWMutex
}

func NewBus() *Bus {
	return &Bus{
		subscribers: make(map[EventType][]chan Event),
	}
}

func (b *Bus) Subscribe(eventType EventType) <-chan Event {
	ch := make(chan Event, 10)

	b.mu.Lock()
	b.subscribers[eventType] = append(b.subscribers[eventType], ch)
	b.mu.Unlock()

	return ch
}

func (b *Bus) Unsubscribe(eventType EventType, ch <-chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	subs := b.subscribers[eventType]
	for i, sub := range subs {
		if sub == ch {
			b.subscribers[eventType] = append(subs[:i], subs[i+1:]...)
			close(sub)
			return
		}
	}
}

func (b *Bus) Publish(evt Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.subscribers[evt.Type] {
		ch <- evt
	}
}
