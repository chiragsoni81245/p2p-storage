//go:build unit

package event

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBus(t *testing.T) {
	bus := NewBus()
	require.NotNil(t, bus)
	assert.NotNil(t, bus.subscribers)
	assert.Empty(t, bus.subscribers)
}

func TestBus_Subscribe(t *testing.T) {
	bus := NewBus()

	ch := bus.Subscribe(PeerDiscovered)

	require.NotNil(t, ch)
	assert.Len(t, bus.subscribers[PeerDiscovered], 1)
}

func TestBus_Subscribe_MultipleSubscribers(t *testing.T) {
	bus := NewBus()

	ch1 := bus.Subscribe(PeerDiscovered)
	ch2 := bus.Subscribe(PeerDiscovered)
	ch3 := bus.Subscribe(PeerConnected)

	require.NotNil(t, ch1)
	require.NotNil(t, ch2)
	require.NotNil(t, ch3)

	assert.Len(t, bus.subscribers[PeerDiscovered], 2)
	assert.Len(t, bus.subscribers[PeerConnected], 1)
}

func TestBus_Unsubscribe(t *testing.T) {
	bus := NewBus()
	ch := bus.Subscribe(PeerDiscovered)

	assert.Len(t, bus.subscribers[PeerDiscovered], 1)

	bus.Unsubscribe(PeerDiscovered, ch)

	assert.Len(t, bus.subscribers[PeerDiscovered], 0)
}

func TestBus_Unsubscribe_OneOfMany(t *testing.T) {
	bus := NewBus()
	ch1 := bus.Subscribe(PeerDiscovered)
	bus.Subscribe(PeerDiscovered)

	bus.Unsubscribe(PeerDiscovered, ch1)

	assert.Len(t, bus.subscribers[PeerDiscovered], 1)
	// Remaining subscriber should still receive events
	remaining := bus.subscribers[PeerDiscovered][0]
	go bus.Publish(Event{Type: PeerDiscovered})
	select {
	case <-remaining:
	case <-time.After(time.Second):
		t.Fatal("remaining subscriber should still receive events after ch1 is unsubscribed")
	}
}

func TestBus_Unsubscribe_ClosesChannel(t *testing.T) {
	bus := NewBus()
	ch := bus.Subscribe(PeerDiscovered)

	bus.Unsubscribe(PeerDiscovered, ch)

	// Channel should be closed after unsubscribe
	_, ok := <-ch
	assert.False(t, ok, "channel should be closed after unsubscribe")
}

func TestBus_Unsubscribe_AlreadyUnsubscribed(t *testing.T) {
	bus := NewBus()
	ch := bus.Subscribe(PeerDiscovered)
	bus.Unsubscribe(PeerDiscovered, ch)

	// Unsubscribing the same channel again should not panic
	assert.NotPanics(t, func() {
		bus.Unsubscribe(PeerDiscovered, ch)
	})
}

func TestBus_Publish_SingleSubscriber(t *testing.T) {
	bus := NewBus()
	ch := bus.Subscribe(PeerDiscovered)

	testData := "test-peer-id"
	go bus.Publish(Event{
		Type: PeerDiscovered,
		Data: testData,
	})

	select {
	case evt := <-ch:
		assert.Equal(t, PeerDiscovered, evt.Type)
		assert.Equal(t, testData, evt.Data)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestBus_Publish_MultipleSubscribers(t *testing.T) {
	bus := NewBus()
	ch1 := bus.Subscribe(PeerDiscovered)
	ch2 := bus.Subscribe(PeerDiscovered)

	testData := "test-peer-id"
	go bus.Publish(Event{
		Type: PeerDiscovered,
		Data: testData,
	})

	for _, ch := range []<-chan Event{ch1, ch2} {
		select {
		case evt := <-ch:
			assert.Equal(t, PeerDiscovered, evt.Type)
			assert.Equal(t, testData, evt.Data)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for event")
		}
	}
}

func TestBus_Publish_DifferentEventTypes(t *testing.T) {
	bus := NewBus()
	chDiscovered := bus.Subscribe(PeerDiscovered)
	chConnected := bus.Subscribe(PeerConnected)

	go bus.Publish(Event{
		Type: PeerDiscovered,
		Data: "discovered",
	})

	select {
	case evt := <-chDiscovered:
		assert.Equal(t, PeerDiscovered, evt.Type)
		assert.Equal(t, "discovered", evt.Data)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for PeerDiscovered event")
	}

	select {
	case <-chConnected:
		t.Fatal("should not receive event on different event type")
	case <-time.After(100 * time.Millisecond):
		// Expected - no event received
	}
}

func TestBus_Publish_WithRequestID(t *testing.T) {
	bus := NewBus()
	ch := bus.Subscribe(FileGetComplete)

	go bus.Publish(Event{
		Type:      FileGetComplete,
		RequestID: "req-123",
		Data:      GetCompleteData{Key: "abc"},
	})

	select {
	case evt := <-ch:
		assert.Equal(t, FileGetComplete, evt.Type)
		assert.Equal(t, "req-123", evt.RequestID)
		data := evt.Data.(GetCompleteData)
		assert.Equal(t, "abc", data.Key)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestBus_Publish_NoSubscribers(t *testing.T) {
	bus := NewBus()

	assert.NotPanics(t, func() {
		bus.Publish(Event{
			Type: PeerDiscovered,
			Data: "test",
		})
	})
}

func TestBus_ConcurrentSubscribePublish(t *testing.T) {
	bus := NewBus()
	const numSubscribers = 10
	const numEvents = 50

	var wg sync.WaitGroup
	channels := make([]<-chan Event, numSubscribers)

	for i := 0; i < numSubscribers; i++ {
		channels[i] = bus.Subscribe(PeerDiscovered)
	}

	received := make([]int, numSubscribers)
	for i := 0; i < numSubscribers; i++ {
		wg.Add(1)
		go func(idx int, ch <-chan Event) {
			defer wg.Done()
			for j := 0; j < numEvents; j++ {
				select {
				case <-ch:
					received[idx]++
				case <-time.After(5 * time.Second):
					return
				}
			}
		}(i, channels[i])
	}

	// Publish in a goroutine to avoid blocking while holding the lock
	// when subscriber channels are temporarily full
	go func() {
		for i := 0; i < numEvents; i++ {
			bus.Publish(Event{
				Type: PeerDiscovered,
				Data: i,
			})
		}
	}()

	wg.Wait()

	for i, count := range received {
		assert.Equal(t, numEvents, count, "subscriber %d did not receive all events", i)
	}
}

func TestEventType_Constants(t *testing.T) {
	assert.Equal(t, EventType("peer:discovered"), PeerDiscovered)
	assert.Equal(t, EventType("peer:connected"), PeerConnected)
	assert.Equal(t, EventType("peer:disconnected"), PeerDisconnected)
	assert.Equal(t, EventType("file:get:started"), FileGetStarted)
	assert.Equal(t, EventType("file:get:progress"), FileGetProgress)
	assert.Equal(t, EventType("file:get:complete"), FileGetComplete)
	assert.Equal(t, EventType("file:get:failed"), FileGetFailed)
}

func TestEvent_Struct(t *testing.T) {
	evt := Event{
		Type:      PeerConnected,
		RequestID: "req-456",
		Data:      map[string]string{"peer": "123"},
	}

	assert.Equal(t, PeerConnected, evt.Type)
	assert.Equal(t, "req-456", evt.RequestID)
	assert.Equal(t, map[string]string{"peer": "123"}, evt.Data)
}

func TestGetStartedData(t *testing.T) {
	data := GetStartedData{Key: "abc123"}
	assert.Equal(t, "abc123", data.Key)
}

func TestGetProgressData(t *testing.T) {
	data := GetProgressData{
		Key:           "abc123",
		BytesReceived: 512,
		TotalBytes:    1024,
	}
	assert.Equal(t, "abc123", data.Key)
	assert.Equal(t, int64(512), data.BytesReceived)
	assert.Equal(t, int64(1024), data.TotalBytes)
}

func TestGetCompleteData(t *testing.T) {
	data := GetCompleteData{Key: "abc123"}
	assert.Equal(t, "abc123", data.Key)
}

func TestGetFailedData(t *testing.T) {
	err := errors.New("transfer failed")
	data := GetFailedData{Key: "abc123", Err: err}
	assert.Equal(t, "abc123", data.Key)
	assert.Equal(t, err, data.Err)
}
