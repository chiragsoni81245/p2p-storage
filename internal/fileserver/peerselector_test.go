//go:build unit

package fileserver

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
)

func makePeers(n int) []peer.ID {
	peers := make([]peer.ID, n)
	for i := range peers {
		peers[i] = peer.ID([]byte{byte(i + 1)})
	}
	return peers
}

func TestRandomPeerSelector_ExcludesSender(t *testing.T) {
	s := &RandomPeerSelector{}
	all := makePeers(5)
	exclude := all[0]

	result := s.Select(all, exclude, 10)
	for _, p := range result {
		assert.NotEqual(t, exclude, p, "excluded peer should not appear in result")
	}
}

func TestRandomPeerSelector_RespectsMax(t *testing.T) {
	s := &RandomPeerSelector{}
	all := makePeers(10)

	result := s.Select(all, "", 3)
	assert.Len(t, result, 3)
}

func TestRandomPeerSelector_MaxBeyondAvailable(t *testing.T) {
	s := &RandomPeerSelector{}
	all := makePeers(3)

	result := s.Select(all, "", 10)
	assert.Len(t, result, 3, "should return all available peers when max exceeds count")
}

func TestRandomPeerSelector_EmptyList(t *testing.T) {
	s := &RandomPeerSelector{}
	result := s.Select([]peer.ID{}, "", 5)
	assert.Empty(t, result)
}

func TestRandomPeerSelector_OnlyExcludedPeer(t *testing.T) {
	s := &RandomPeerSelector{}
	all := makePeers(1)
	result := s.Select(all, all[0], 5)
	assert.Empty(t, result)
}

func TestRandomPeerSelector_NoDuplicates(t *testing.T) {
	s := &RandomPeerSelector{}
	all := makePeers(10)

	result := s.Select(all, "", 10)
	seen := make(map[peer.ID]bool)
	for _, p := range result {
		assert.False(t, seen[p], "result should contain no duplicate peers")
		seen[p] = true
	}
}
