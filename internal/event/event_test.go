//go:build unit

package event

import (
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

	// Only discovered subscriber should receive
	select {
	case evt := <-chDiscovered:
		assert.Equal(t, PeerDiscovered, evt.Type)
		assert.Equal(t, "discovered", evt.Data)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for PeerDiscovered event")
	}

	// Connected subscriber should not receive anything
	select {
	case <-chConnected:
		t.Fatal("should not receive event on different event type")
	case <-time.After(100 * time.Millisecond):
		// Expected - no event received
	}
}

func TestBus_Publish_NoSubscribers(t *testing.T) {
	bus := NewBus()

	// Should not panic
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

	// Create subscribers
	for i := 0; i < numSubscribers; i++ {
		channels[i] = bus.Subscribe(PeerDiscovered)
	}

	// Start receivers
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

	// Publish events
	for i := 0; i < numEvents; i++ {
		bus.Publish(Event{
			Type: PeerDiscovered,
			Data: i,
		})
	}

	wg.Wait()

	// Verify all subscribers received all events
	for i, count := range received {
		assert.Equal(t, numEvents, count, "subscriber %d did not receive all events", i)
	}
}

func TestEventType_Constants(t *testing.T) {
	assert.Equal(t, EventType("peer:discovered"), PeerDiscovered)
	assert.Equal(t, EventType("peer:connected"), PeerConnected)
	assert.Equal(t, EventType("peer:disconnected"), PeerDisconnected)
}

func TestEvent_Struct(t *testing.T) {
	evt := Event{
		Type: PeerConnected,
		Data: map[string]string{"peer": "123"},
	}

	assert.Equal(t, PeerConnected, evt.Type)
	assert.Equal(t, map[string]string{"peer": "123"}, evt.Data)
}
