package network

import (
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockClock implements Clock interface for testing
type MockClock struct {
	mu   sync.Mutex
	time time.Time
}

func NewMockClock(t time.Time) *MockClock {
	return &MockClock{time: t}
}

func (m *MockClock) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.time
}

func (m *MockClock) Advance(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.time = m.time.Add(d)
}

func (m *MockClock) Set(t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.time = t
}

func TestNewPeerScorer(t *testing.T) {
	ps := NewPeerScorer()
	require.NotNil(t, ps)
	assert.NotNil(t, ps.scores)
	assert.Empty(t, ps.scores)
}

func TestNewPeerScorerWithClock(t *testing.T) {
	clock := NewMockClock(time.Now())
	ps := NewPeerScorerWithClock(clock)

	require.NotNil(t, ps)
	assert.Equal(t, clock, ps.clock)
}

func TestPeerScorer_RecordSuccess(t *testing.T) {
	clock := NewMockClock(time.Now())
	ps := NewPeerScorerWithClock(clock)

	peerID := peer.ID("test-peer")
	latency := 100 * time.Millisecond

	ps.RecordSuccess(peerID, latency)

	ps.mu.RLock()
	score, ok := ps.scores[peerID]
	ps.mu.RUnlock()

	require.True(t, ok)
	assert.Equal(t, float64(1), score.WeightedSuccess)
	assert.Equal(t, float64(0), score.WeightedFailure)
	assert.Equal(t, latency, score.AvgLatency)
}

func TestPeerScorer_RecordSuccess_LatencyAverage(t *testing.T) {
	clock := NewMockClock(time.Now())
	ps := NewPeerScorerWithClock(clock)

	peerID := peer.ID("test-peer")

	ps.RecordSuccess(peerID, 100*time.Millisecond)
	ps.RecordSuccess(peerID, 200*time.Millisecond)

	ps.mu.RLock()
	score := ps.scores[peerID]
	ps.mu.RUnlock()

	// EMA: alpha=0.3, first=100ms sets avg, second=200ms → 0.7*100 + 0.3*200 = 130ms
	assert.Equal(t, 130*time.Millisecond, score.AvgLatency)
}

func TestPeerScorer_RecordFailure(t *testing.T) {
	clock := NewMockClock(time.Now())
	ps := NewPeerScorerWithClock(clock)

	peerID := peer.ID("test-peer")

	ps.RecordFailure(peerID)

	ps.mu.RLock()
	score, ok := ps.scores[peerID]
	ps.mu.RUnlock()

	require.True(t, ok)
	assert.Equal(t, float64(0), score.WeightedSuccess)
	assert.Equal(t, float64(1), score.WeightedFailure)
}

func TestPeerScorer_GetScore_NoPeer(t *testing.T) {
	ps := NewPeerScorer()

	score := ps.GetScore(peer.ID("unknown"))
	assert.Equal(t, float64(0), score)
}

func TestPeerScorer_GetScore_SuccessMinusFailure(t *testing.T) {
	clock := NewMockClock(time.Now())
	ps := NewPeerScorerWithClock(clock)

	peerID := peer.ID("test-peer")

	// 5 successes, 2 failures
	for i := 0; i < 5; i++ {
		ps.RecordSuccess(peerID, 0) // Zero latency to simplify
	}
	for i := 0; i < 2; i++ {
		ps.RecordFailure(peerID)
	}

	score := ps.GetScore(peerID)
	// Score = 5 - 2 - latency_penalty
	// With zero latency, should be 3.0
	assert.InDelta(t, 3.0, score, 0.01)
}

func TestPeerScorer_GetScore_LatencyPenalty(t *testing.T) {
	clock := NewMockClock(time.Now())
	ps := NewPeerScorerWithClock(clock)

	peerID := peer.ID("test-peer")

	// 1 success with 1 second latency
	ps.RecordSuccess(peerID, time.Second)

	score := ps.GetScore(peerID)
	// Score = 1 - 0 - 0.1 (latency penalty = 1s * 0.1 weight)
	assert.InDelta(t, 0.9, score, 0.01)
}

func TestPeerScorer_Decay(t *testing.T) {
	baseTime := time.Now()
	clock := NewMockClock(baseTime)
	ps := NewPeerScorerWithClock(clock)

	peerID := peer.ID("test-peer")

	// Record initial success
	ps.RecordSuccess(peerID, 0)

	initialScore := ps.GetScore(peerID)
	assert.InDelta(t, 1.0, initialScore, 0.01)

	// Advance time by decay half-life (5 minutes)
	// The decay formula is exp(-t/halfLife) so at t=halfLife, factor = exp(-1) ≈ 0.368
	clock.Advance(5 * time.Minute)

	decayedScore := ps.GetScore(peerID)
	// After one "half-life", score should be approximately exp(-1) ≈ 0.368
	assert.InDelta(t, 0.368, decayedScore, 0.05)

	// Advance another half-life
	clock.Advance(5 * time.Minute)

	furtherDecayedScore := ps.GetScore(peerID)
	// After two "half-lives", score should be approximately exp(-2) ≈ 0.135
	assert.InDelta(t, 0.135, furtherDecayedScore, 0.05)
}

func TestPeerScorer_BestPeers(t *testing.T) {
	clock := NewMockClock(time.Now())
	ps := NewPeerScorerWithClock(clock)

	peerA := peer.ID("peer-A")
	peerB := peer.ID("peer-B")
	peerC := peer.ID("peer-C")

	// Give different scores
	ps.RecordSuccess(peerA, 0) // Score: 1
	ps.RecordSuccess(peerB, 0)
	ps.RecordSuccess(peerB, 0)
	ps.RecordSuccess(peerB, 0) // Score: 3
	ps.RecordFailure(peerC)    // Score: -1

	peers := []peer.ID{peerA, peerB, peerC}
	best := ps.BestPeers(peers, 2)

	require.Len(t, best, 2)
	assert.Equal(t, peerB, best[0], "peerB should be first (highest score)")
	assert.Equal(t, peerA, best[1], "peerA should be second")
}

func TestPeerScorer_BestPeers_MoreThanAvailable(t *testing.T) {
	clock := NewMockClock(time.Now())
	ps := NewPeerScorerWithClock(clock)

	peerA := peer.ID("peer-A")
	ps.RecordSuccess(peerA, 0)

	peers := []peer.ID{peerA}
	best := ps.BestPeers(peers, 10) // Request more than available

	require.Len(t, best, 1)
	assert.Equal(t, peerA, best[0])
}

func TestPeerScorer_BestPeers_EmptyList(t *testing.T) {
	ps := NewPeerScorer()

	best := ps.BestPeers([]peer.ID{}, 5)
	assert.Empty(t, best)
}

func TestPeerScorer_BestPeers_UnknownPeers(t *testing.T) {
	ps := NewPeerScorer()

	// Peers that have no recorded scores
	peers := []peer.ID{peer.ID("unknown-1"), peer.ID("unknown-2")}
	best := ps.BestPeers(peers, 2)

	// Should return all peers (all have score 0)
	assert.Len(t, best, 2)
}

// TestPeerScorer_ConcurrentAccess verifies thread safety.
// Run with: go test -race -run TestPeerScorer_ConcurrentAccess
func TestPeerScorer_ConcurrentAccess(t *testing.T) {
	ps := NewPeerScorer()

	numGoroutines := 10
	numOps := 100

	allPeers := make([]peer.ID, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		allPeers[i] = peer.ID("peer-" + string(rune('A'+i)))
	}

	done := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				peerID := allPeers[idx]
				for j := 0; j < numOps; j++ {
					switch j % 4 {
					case 0:
						ps.RecordSuccess(peerID, time.Millisecond*time.Duration(j))
					case 1:
						ps.RecordFailure(peerID)
					case 2:
						_ = ps.GetScore(peerID)
					case 3:
						_ = ps.BestPeers(allPeers, 3)
					}
				}
			}(i)
		}
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - no deadlock
	case <-time.After(5 * time.Second):
		t.Fatal("test timed out - possible deadlock")
	}

	// Verify state consistency after concurrent access
	for _, p := range allPeers {
		score := ps.GetScore(p)
		assert.False(t, score != score, "score should not be NaN") // NaN != NaN
	}

	best := ps.BestPeers(allPeers, numGoroutines)
	assert.Len(t, best, numGoroutines, "BestPeers should return all peers")
}

func TestDecayFactor(t *testing.T) {
	// The decay formula is exp(-elapsed/halfLife), not traditional half-life
	// After 5 minutes (decayHalfLife): exp(-1) ≈ 0.368
	// After 10 minutes: exp(-2) ≈ 0.135
	tests := []struct {
		name     string
		elapsed  time.Duration
		expected float64
		delta    float64
	}{
		{"no time", 0, 1.0, 0.01},
		{"half-life period", 5 * time.Minute, 0.368, 0.01}, // exp(-1)
		{"two periods", 10 * time.Minute, 0.135, 0.01},     // exp(-2)
		{"three periods", 15 * time.Minute, 0.05, 0.01},    // exp(-3)
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			factor := decayFactor(tc.elapsed)
			assert.InDelta(t, tc.expected, factor, tc.delta)
		})
	}
}

func TestRealClock(t *testing.T) {
	clock := RealClock{}
	before := time.Now()
	now := clock.Now()
	after := time.Now()

	assert.True(t, !now.Before(before), "clock.Now() should be >= before")
	assert.True(t, !now.After(after), "clock.Now() should be <= after")
}
