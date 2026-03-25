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
)

type Event struct {
	Type EventType
	Data any
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

func (b *Bus) Publish(evt Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.subscribers[evt.Type] {
		ch <- evt
	}
}
