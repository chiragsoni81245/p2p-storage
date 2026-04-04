package fileserver

import (
	"math/rand"

	"github.com/libp2p/go-libp2p/core/peer"
)

// PeerSelector chooses which peers to forward a GET_FILE message to.
type PeerSelector interface {
	Select(all []peer.ID, exclude peer.ID, max int) []peer.ID
}

// RandomPeerSelector selects up to max peers uniformly at random, excluding one peer.
type RandomPeerSelector struct{}

func (r *RandomPeerSelector) Select(all []peer.ID, exclude peer.ID, max int) []peer.ID {
	// Copy slice to avoid mutating the original
	candidates := make([]peer.ID, 0, len(all))
	for _, p := range all {
		if p != exclude {
			candidates = append(candidates, p)
		}
	}

	// Fisher-Yates partial shuffle — only shuffle as many positions as we need
	n := len(candidates)
	if max > n {
		max = n
	}
	for i := 0; i < max; i++ {
		j := i + rand.Intn(n-i)
		candidates[i], candidates[j] = candidates[j], candidates[i]
	}

	return candidates[:max]
}
