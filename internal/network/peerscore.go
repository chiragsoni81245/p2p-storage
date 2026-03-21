package network

import (
	"math"
	"sort"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

type Score struct {
	WeightedSuccess float64
	WeightedFailure float64
	AvgLatency   time.Duration
	LastUpdated  time.Time
}

const decayHalfLife = 5 * time.Minute // tune this

func decayFactor(elapsed time.Duration) float64 {
	return math.Exp(-elapsed.Seconds() / decayHalfLife.Seconds())
}

type PeerScorer struct {
	mu     sync.RWMutex
	scores map[peer.ID]*Score
}

func NewPeerScorer() *PeerScorer {
	return &PeerScorer{
		scores: make(map[peer.ID]*Score),
	}
}

func (ps *PeerScorer) applyDecay(s *Score) {
	now := time.Now()
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
		s = &Score{LastUpdated: time.Now()}
		ps.scores[id] = s
	}

	ps.applyDecay(s)

	s.WeightedSuccess += 1

	// latency moving average
	if s.AvgLatency == 0 {
		s.AvgLatency = latency
	} else {
		s.AvgLatency = (s.AvgLatency + latency) / 2
	}
}

func (ps *PeerScorer) RecordFailure(id peer.ID) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	s, ok := ps.scores[id]
	if !ok {
		s = &Score{LastUpdated: time.Now()}
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

	score := s.WeightedSuccess - s.WeightedFailure

	// latency penalty
	if s.AvgLatency > 0 {
		score -= s.AvgLatency.Seconds()
	}

	return score
}

func (ps *PeerScorer) BestPeers(peers []peer.ID, n int) []peer.ID {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	type pair struct {
		id    peer.ID
		score float64
	}

	var list []pair

	for _, p := range peers {
		list = append(list, pair{
			id:    p,
			score: ps.GetScore(p),
		})
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
