//go:build unit

package fileserver

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSeenMessages_NewIDs(t *testing.T) {
	s := NewSeenMessages(100, time.Minute)
	assert.True(t, s.See("a"), "first time should be new")
	assert.True(t, s.See("b"), "different id should be new")
}

func TestSeenMessages_Duplicate(t *testing.T) {
	s := NewSeenMessages(100, time.Minute)
	assert.True(t, s.See("x"))
	assert.False(t, s.See("x"), "same id within window should be duplicate")
}

func TestSeenMessages_ExpiredID(t *testing.T) {
	s := NewSeenMessages(100, 50*time.Millisecond)
	assert.True(t, s.See("x"))
	time.Sleep(60 * time.Millisecond)
	assert.True(t, s.See("x"), "id should be new again after window expires")
}

func TestSeenMessages_EvictsOldestAtCap(t *testing.T) {
	s := NewSeenMessages(3, time.Minute)
	s.See("a")
	time.Sleep(2 * time.Millisecond) // ensure ordering
	s.See("b")
	time.Sleep(2 * time.Millisecond)
	s.See("c")

	// At cap — adding "d" should evict "a" (oldest)
	assert.True(t, s.See("d"), "d is new")
	// "a" was evicted, so it should be seen as new again
	assert.True(t, s.See("a"), "oldest entry should have been evicted")
}

func TestSeenMessages_ConcurrentSafe(t *testing.T) {
	s := NewSeenMessages(1000, time.Minute)
	done := make(chan struct{})
	for i := 0; i < 50; i++ {
		go func(i int) {
			s.See(fmt.Sprintf("id-%d", i))
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 50; i++ {
		<-done
	}
}
