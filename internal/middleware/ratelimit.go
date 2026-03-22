package middleware

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/core"

	"github.com/libp2p/go-libp2p/core/peer"
)

type peerWindow struct {
	requests []time.Time
	lastSeen time.Time
}

type RateLimiter struct {
	mu       sync.Mutex
	peers    map[peer.ID]*peerWindow
	limit    int           // max requests
	window   time.Duration // time window
	ttl      time.Duration // cleanup TTL
	stopChan chan struct{}
}

func NewRateLimiter(limit int, window, ttl time.Duration) *RateLimiter {
	rl := &RateLimiter{
		peers:    make(map[peer.ID]*peerWindow),
		limit:    limit,
		window:   window,
		ttl:      ttl,
		stopChan: make(chan struct{}),
	}

	go rl.cleanupLoop()

	return rl
}

func (rl *RateLimiter) Wrap(next core.Handler) core.Handler {
	return core.HandlerFunc(func(ctx context.Context, peerID peer.ID, msg core.Message) (core.Message, error) {
		if !rl.allow(peerID) {
			return nil, fmt.Errorf("rate limit exceeded")
		}
		return next.Handle(ctx, peerID, msg)
	})
}

func (rl *RateLimiter) allow(id peer.ID) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	pw, ok := rl.peers[id]
	if !ok {
		pw = &peerWindow{}
		rl.peers[id] = pw
	}

	pw.lastSeen = now

	// remove old requests
	cutoff := now.Add(-rl.window)
	valid := pw.requests[:0]

	for _, t := range pw.requests {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	pw.requests = valid

	if len(pw.requests) >= rl.limit {
		return false
	}

	pw.requests = append(pw.requests, now)
	return true
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.ttl)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-rl.stopChan:
			return
		}
	}
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	for id, pw := range rl.peers {
		if now.Sub(pw.lastSeen) > rl.ttl {
			delete(rl.peers, id)
		}
	}
}

// Stop gracefully shuts down the rate limiter's cleanup goroutine.
// Should be called when the rate limiter is no longer needed.
func (rl *RateLimiter) Stop() {
	close(rl.stopChan)
}
