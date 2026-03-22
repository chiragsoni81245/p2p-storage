package network

import (
	"math"
	"sort"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// Clock abstracts time operations for testability
type Clock interface {
	Now() time.Time
}

// RealClock implements Clock using actual system time
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }

type Score struct {
	WeightedSuccess float64
	WeightedFailure float64
	AvgLatency      time.Duration
	LastUpdated     time.Time
}

const decayHalfLife = 5 * time.Minute // tune this
const latencyPenaltyWeight = 0.1      // weight for latency penalty
const latencyEMAAlpha = 0.3           // exponential moving average alpha for latency

func decayFactor(elapsed time.Duration) float64 {
	return math.Exp(-elapsed.Seconds() / decayHalfLife.Seconds())
}

// computeScore calculates the final score from a Score struct
func computeScore(s *Score) float64 {
	score := s.WeightedSuccess - s.WeightedFailure
	if s.AvgLatency > 0 {
		score -= s.AvgLatency.Seconds() * latencyPenaltyWeight
	}
	return score
}

type PeerScorer struct {
	mu     sync.RWMutex
	scores map[peer.ID]*Score
	clock  Clock
}

func NewPeerScorer() *PeerScorer {
	return &PeerScorer{
		scores: make(map[peer.ID]*Score),
		clock:  RealClock{},
	}
}

// NewPeerScorerWithClock creates a PeerScorer with a custom clock (for testing)
func NewPeerScorerWithClock(clock Clock) *PeerScorer {
	return &PeerScorer{
		scores: make(map[peer.ID]*Score),
		clock:  clock,
	}
}

func (ps *PeerScorer) applyDecay(s *Score) {
	now := ps.clock.Now()
	elapsed := now.Sub(s.LastUpdated)

	if elapsed <= 0 {
		return
	}

	factor := decayFactor(elapsed)

	s.WeightedSuccess *= factor
	s.WeightedFailure *= factor

	s.LastUpdated = now
}

func (ps *PeerScorer) RecordSuccess(id peer.ID, latency time.Duration) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	s, ok := ps.scores[id]
	if !ok {
		s = &Score{LastUpdated: ps.clock.Now()}
		ps.scores[id] = s
	}

	ps.applyDecay(s)

	s.WeightedSuccess += 1

	// latency exponential moving average
	if s.AvgLatency == 0 {
		s.AvgLatency = latency
	} else {
		s.AvgLatency = time.Duration(float64(s.AvgLatency)*(1-latencyEMAAlpha) + float64(latency)*latencyEMAAlpha)
	}
}

func (ps *PeerScorer) RecordFailure(id peer.ID) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	s, ok := ps.scores[id]
	if !ok {
		s = &Score{LastUpdated: ps.clock.Now()}
		ps.scores[id] = s
	}

	ps.applyDecay(s)

	s.WeightedFailure += 1
}

func (ps *PeerScorer) GetScore(id peer.ID) float64 {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	s, ok := ps.scores[id]
	if !ok {
		return 0
	}

	ps.applyDecay(s)

	return computeScore(s)
}

func (ps *PeerScorer) BestPeers(peers []peer.ID, n int) []peer.ID {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	type pair struct {
		id    peer.ID
		score float64
	}

	var list []pair

	for _, p := range peers {
		s, ok := ps.scores[p]
		if !ok {
			list = append(list, pair{id: p, score: 0})
			continue
		}

		ps.applyDecay(s)

		list = append(list, pair{id: p, score: computeScore(s)})
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].score > list[j].score
	})

	var result []peer.ID
	for i := 0; i < len(list) && i < n; i++ {
		result = append(result, list[i].id)
	}

	return result
}
