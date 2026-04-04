package fileserver

import (
	"sync"
	"time"
)

// SeenMessages is a sliding-window deduplication store for message IDs.
// It is safe for concurrent use.
type SeenMessages struct {
	mu      sync.Mutex
	entries map[string]time.Time
	maxSize int
	window  time.Duration
}

func NewSeenMessages(maxSize int, window time.Duration) *SeenMessages {
	s := &SeenMessages{
		entries: make(map[string]time.Time, maxSize),
		maxSize: maxSize,
		window:  window,
	}
	go s.cleanup()
	return s
}

// See returns true if id is new (not seen within the window) and records it.
// Returns false if id was already seen. When at capacity, evicts the oldest entry.
func (s *SeenMessages) See(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	if t, ok := s.entries[id]; ok && now.Sub(t) < s.window {
		return false
	}

	if len(s.entries) >= s.maxSize {
		// Evict oldest entry
		var oldest string
		var oldestTime time.Time
		for k, v := range s.entries {
			if oldest == "" || v.Before(oldestTime) {
				oldest = k
				oldestTime = v
			}
		}
		delete(s.entries, oldest)
	}

	s.entries[id] = now
	return true
}

func (s *SeenMessages) cleanup() {
	ticker := time.NewTicker(s.window / 2)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for id, t := range s.entries {
			if now.Sub(t) >= s.window {
				delete(s.entries, id)
			}
		}
		s.mu.Unlock()
	}
}
