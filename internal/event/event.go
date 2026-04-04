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

	// Receive session events
	FileReceiveStarted  EventType = "file:receive:started"
	FileReceiveProgress EventType = "file:receive:progress"
	FileReceiveComplete EventType = "file:receive:complete"
	FileReceiveFailed   EventType = "file:receive:failed"

	// Get (hop) events
	GetReceived   EventType = "get:received"   // relaying: don't have file, forwarding
	GetDelivering EventType = "get:delivering" // have file, connecting back to requester
	GetDelivered  EventType = "get:delivered"  // delivery complete
)

type Event struct {
	Type      EventType
	RequestID string
	Data      any
}

// ReceiveStartedData is published when an incoming file transfer begins
type ReceiveStartedData struct {
	Key    string
	Size   int64
	PeerID string
}

// ReceiveProgressData is published periodically during an incoming transfer
type ReceiveProgressData struct {
	Key           string
	BytesReceived int64
	TotalBytes    int64
}

// ReceiveCompleteData is published when an incoming transfer finishes successfully
type ReceiveCompleteData struct {
	Key  string
	Size int64
}

// ReceiveFailedData is published when an incoming transfer fails
type ReceiveFailedData struct {
	Key string
	Err error
}

// GetReceivedData is published when this node receives a get request it doesn't have and forwards it
type GetReceivedData struct {
	MsgID string
	Key   string
	TTL   int
}

// GetDeliveringData is published when this node has the file and is connecting back to the requester
type GetDeliveringData struct {
	MsgID       string
	Key         string
	RequesterID string
}

// GetDeliveredData is published when delivery to the requester completed successfully
type GetDeliveredData struct {
	MsgID string
	Key   string
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
