package middleware

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/core"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter(10, time.Second, time.Minute)
	defer rl.Stop()

	require.NotNil(t, rl)
	assert.Equal(t, 10, rl.limit)
	assert.Equal(t, time.Second, rl.window)
	assert.Equal(t, time.Minute, rl.ttl)
	assert.NotNil(t, rl.peers)
	assert.NotNil(t, rl.stopChan)
}

func TestRateLimiter_Allow_AtLimit(t *testing.T) {
	rl := NewRateLimiter(3, time.Second, time.Minute)
	defer rl.Stop()

	peerID := peer.ID("test-peer")

	// Fill up the limit
	for i := 0; i < 3; i++ {
		assert.True(t, rl.allow(peerID))
	}

	// Further requests should be denied
	assert.False(t, rl.allow(peerID))
	assert.False(t, rl.allow(peerID))
}

func TestRateLimiter_Allow_WindowExpiry(t *testing.T) {
	window := 100 * time.Millisecond
	rl := NewRateLimiter(2, window, time.Minute)
	defer rl.Stop()

	peerID := peer.ID("test-peer")

	// Use up the limit
	assert.True(t, rl.allow(peerID))
	assert.True(t, rl.allow(peerID))
	assert.False(t, rl.allow(peerID))

	// Wait for window to expire
	time.Sleep(window + 50*time.Millisecond)

	// Should be allowed again
	assert.True(t, rl.allow(peerID))
}

func TestRateLimiter_Allow_MultiplePeers(t *testing.T) {
	rl := NewRateLimiter(2, time.Second, time.Minute)
	defer rl.Stop()

	peer1 := peer.ID("peer-1")
	peer2 := peer.ID("peer-2")

	// Each peer has their own limit
	assert.True(t, rl.allow(peer1))
	assert.True(t, rl.allow(peer1))
	assert.False(t, rl.allow(peer1))

	// Peer2 should still have their limit available
	assert.True(t, rl.allow(peer2))
	assert.True(t, rl.allow(peer2))
	assert.False(t, rl.allow(peer2))
}

func TestRateLimiter_Wrap_AllowedRequest(t *testing.T) {
	rl := NewRateLimiter(5, time.Second, time.Minute)
	defer rl.Stop()

	handlerCalled := false
	baseHandler := core.HandlerFunc(func(ctx context.Context, peerID peer.ID, msg core.Message) (core.Message, error) {
		handlerCalled = true
		return "response", nil
	})

	wrapped := rl.Wrap(baseHandler)

	resp, err := wrapped.Handle(context.Background(), peer.ID("test"), "msg")

	require.NoError(t, err)
	assert.True(t, handlerCalled)
	assert.Equal(t, "response", resp)
}

func TestRateLimiter_Wrap_RateLimited(t *testing.T) {
	rl := NewRateLimiter(1, time.Second, time.Minute)
	defer rl.Stop()

	handlerCalled := 0
	baseHandler := core.HandlerFunc(func(ctx context.Context, peerID peer.ID, msg core.Message) (core.Message, error) {
		handlerCalled++
		return "response", nil
	})

	wrapped := rl.Wrap(baseHandler)
	peerID := peer.ID("test")

	// First request should succeed
	resp, err := wrapped.Handle(context.Background(), peerID, "msg")
	require.NoError(t, err)
	assert.Equal(t, "response", resp)
	assert.Equal(t, 1, handlerCalled)

	// Second request should be rate limited
	resp, err = wrapped.Handle(context.Background(), peerID, "msg2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit exceeded")
	assert.Nil(t, resp)
	assert.Equal(t, 1, handlerCalled) // Handler should not be called
}

func TestRateLimiter_Wrap_HandlerError(t *testing.T) {
	rl := NewRateLimiter(5, time.Second, time.Minute)
	defer rl.Stop()

	expectedErr := errors.New("handler error")
	baseHandler := core.HandlerFunc(func(ctx context.Context, peerID peer.ID, msg core.Message) (core.Message, error) {
		return nil, expectedErr
	})

	wrapped := rl.Wrap(baseHandler)

	resp, err := wrapped.Handle(context.Background(), peer.ID("test"), "msg")

	assert.Equal(t, expectedErr, err)
	assert.Nil(t, resp)
}

func TestRateLimiter_Cleanup(t *testing.T) {
	ttl := 100 * time.Millisecond
	rl := NewRateLimiter(5, time.Second, ttl)
	defer rl.Stop()

	peer1 := peer.ID("peer-1")
	peer2 := peer.ID("peer-2")

	// Add some requests
	rl.allow(peer1)
	rl.allow(peer2)

	// Both peers should be in the map
	rl.mu.Lock()
	assert.Contains(t, rl.peers, peer1)
	assert.Contains(t, rl.peers, peer2)
	rl.mu.Unlock()

	// Wait for TTL expiry and cleanup loop to run (loop ticks every ttl)
	time.Sleep(ttl * 3)

	// Peers should be cleaned up by the cleanup loop
	rl.mu.Lock()
	assert.NotContains(t, rl.peers, peer1)
	assert.NotContains(t, rl.peers, peer2)
	rl.mu.Unlock()
}

func TestRateLimiter_Cleanup_KeepsActivePeers(t *testing.T) {
	window := 100 * time.Millisecond
	ttl := 500 * time.Millisecond
	rl := NewRateLimiter(5, window, ttl)
	defer rl.Stop()

	activePeer := peer.ID("active-peer")
	stalePeer := peer.ID("stale-peer")

	// Add stale peer
	rl.allow(stalePeer)

	// Wait for stale peer to become old
	time.Sleep(ttl + 50*time.Millisecond)

	// Add active peer (recent)
	rl.allow(activePeer)

	// Wait for cleanup loop to run once (ticks every ttl)
	time.Sleep(ttl + 100*time.Millisecond)

	rl.mu.Lock()
	_, activeExists := rl.peers[activePeer]
	_, staleExists := rl.peers[stalePeer]
	rl.mu.Unlock()

	assert.True(t, activeExists, "active peer should be kept")
	assert.False(t, staleExists, "stale peer should be removed")
}

func TestRateLimiter_Stop(t *testing.T) {
	rl := NewRateLimiter(5, time.Second, time.Minute)

	// Stop should not panic and should close the channel
	assert.NotPanics(t, func() {
		rl.Stop()
	})

	// Channel should be closed
	select {
	case _, ok := <-rl.stopChan:
		assert.False(t, ok, "stop channel should be closed")
	default:
		t.Fatal("stop channel should be closed, not blocking")
	}
}

func TestRateLimiter_SlidingWindow(t *testing.T) {
	window := 100 * time.Millisecond
	rl := NewRateLimiter(3, window, time.Minute)
	defer rl.Stop()

	peerID := peer.ID("test-peer")

	// Make first request
	assert.True(t, rl.allow(peerID))

	// Wait half the window
	time.Sleep(window / 2)

	// Make two more requests
	assert.True(t, rl.allow(peerID))
	assert.True(t, rl.allow(peerID))
	assert.False(t, rl.allow(peerID)) // At limit

	// Wait for first request to slide out of window
	time.Sleep(window/2 + 10*time.Millisecond)

	// Should be able to make one more request
	assert.True(t, rl.allow(peerID))
}
