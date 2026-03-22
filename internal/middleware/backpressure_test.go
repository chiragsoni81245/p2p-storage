package middleware

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLimiter(t *testing.T) {
	limiter := NewLimiter(5)
	require.NotNil(t, limiter)
	assert.NotNil(t, limiter.sem)
}

func TestLimiter_Acquire_Success(t *testing.T) {
	limiter := NewLimiter(3)

	// Should acquire all 3 slots
	assert.True(t, limiter.Acquire())
	assert.True(t, limiter.Acquire())
	assert.True(t, limiter.Acquire())
}

func TestLimiter_Acquire_Failure(t *testing.T) {
	limiter := NewLimiter(2)

	// Acquire all slots
	assert.True(t, limiter.Acquire())
	assert.True(t, limiter.Acquire())

	// Third acquire should fail (overloaded)
	assert.False(t, limiter.Acquire())
}

func TestLimiter_Release(t *testing.T) {
	limiter := NewLimiter(1)

	// Acquire the only slot
	assert.True(t, limiter.Acquire())
	assert.False(t, limiter.Acquire())

	// Release the slot
	limiter.Release()

	// Now we can acquire again
	assert.True(t, limiter.Acquire())
}

func TestLimiter_AcquireRelease_Cycle(t *testing.T) {
	limiter := NewLimiter(2)

	for i := 0; i < 10; i++ {
		assert.True(t, limiter.Acquire(), "iteration %d: first acquire failed", i)
		assert.True(t, limiter.Acquire(), "iteration %d: second acquire failed", i)
		assert.False(t, limiter.Acquire(), "iteration %d: should be overloaded", i)

		limiter.Release()
		limiter.Release()
	}
}

func TestLimiter_ZeroCapacity(t *testing.T) {
	limiter := NewLimiter(0)

	// Should immediately report overloaded
	assert.False(t, limiter.Acquire())
}
