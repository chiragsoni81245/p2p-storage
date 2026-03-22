package observability

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMetrics(t *testing.T) {
	m := NewMetrics()
	require.NotNil(t, m)
	assert.Equal(t, 0, m.RequestsTotal)
	assert.Equal(t, 0, m.FailuresTotal)
	assert.Equal(t, 0, m.RequestCount)
	assert.Equal(t, time.Duration(0), m.TotalLatency)
}

func TestMetrics_RecordRequest(t *testing.T) {
	m := NewMetrics()

	m.RecordRequest(100 * time.Millisecond)

	assert.Equal(t, 1, m.RequestsTotal)
	assert.Equal(t, 1, m.RequestCount)
	assert.Equal(t, 100*time.Millisecond, m.TotalLatency)
}

func TestMetrics_RecordRequest_Multiple(t *testing.T) {
	m := NewMetrics()

	m.RecordRequest(100 * time.Millisecond)
	m.RecordRequest(200 * time.Millisecond)
	m.RecordRequest(300 * time.Millisecond)

	assert.Equal(t, 3, m.RequestsTotal)
	assert.Equal(t, 3, m.RequestCount)
	assert.Equal(t, 600*time.Millisecond, m.TotalLatency)
}

func TestMetrics_RecordFailure(t *testing.T) {
	m := NewMetrics()

	m.RecordFailure()

	assert.Equal(t, 1, m.FailuresTotal)
	assert.Equal(t, 0, m.RequestsTotal) // Not incremented
}

func TestMetrics_RecordFailure_Multiple(t *testing.T) {
	m := NewMetrics()

	for i := 0; i < 5; i++ {
		m.RecordFailure()
	}

	assert.Equal(t, 5, m.FailuresTotal)
}

func TestMetrics_Snapshot(t *testing.T) {
	m := NewMetrics()

	m.RecordRequest(100 * time.Millisecond)
	m.RecordRequest(200 * time.Millisecond)
	m.RecordFailure()

	snapshot := m.Snapshot()

	assert.Equal(t, 2, snapshot["requests_total"])
	assert.Equal(t, 1, snapshot["failures_total"])
	assert.Equal(t, 150*time.Millisecond, snapshot["avg_latency"])
}

func TestMetrics_Snapshot_NoRequests(t *testing.T) {
	m := NewMetrics()

	snapshot := m.Snapshot()

	assert.Equal(t, 0, snapshot["requests_total"])
	assert.Equal(t, 0, snapshot["failures_total"])
	assert.Equal(t, time.Duration(0), snapshot["avg_latency"])
}

func TestMetrics_Snapshot_ZeroLatency(t *testing.T) {
	m := NewMetrics()
	m.RecordRequest(0)

	snapshot := m.Snapshot()

	assert.Equal(t, 1, snapshot["requests_total"])
	assert.Equal(t, time.Duration(0), snapshot["avg_latency"])
}

func TestMetrics_AverageLatencyCalculation(t *testing.T) {
	m := NewMetrics()

	// Record requests with known latencies
	m.RecordRequest(10 * time.Millisecond)
	m.RecordRequest(20 * time.Millisecond)
	m.RecordRequest(30 * time.Millisecond)
	m.RecordRequest(40 * time.Millisecond)

	snapshot := m.Snapshot()

	// Average: (10 + 20 + 30 + 40) / 4 = 25ms
	assert.Equal(t, 25*time.Millisecond, snapshot["avg_latency"])
}

func TestMetrics_ConcurrentAccess(t *testing.T) {
	m := NewMetrics()

	var wg sync.WaitGroup
	numGoroutines := 10
	numOps := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				if j%3 == 0 {
					m.RecordFailure()
				} else {
					m.RecordRequest(time.Millisecond * time.Duration(j))
				}
				_ = m.Snapshot()
			}
		}()
	}

	wg.Wait()

	// Verify totals are reasonable (not checking exact values due to concurrency)
	snapshot := m.Snapshot()
	assert.Greater(t, snapshot["requests_total"], 0)
	assert.Greater(t, snapshot["failures_total"], 0)
}

func TestMetrics_ThreadSafety(t *testing.T) {
	m := NewMetrics()

	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 1000; i++ {
			m.RecordRequest(time.Millisecond)
			m.RecordFailure()
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 1000; i++ {
			_ = m.Snapshot()
		}
		done <- true
	}()

	<-done
	<-done

	// If no race condition, test passes
}

func TestMetrics_SnapshotDoesNotMutate(t *testing.T) {
	m := NewMetrics()
	m.RecordRequest(100 * time.Millisecond)

	snapshot1 := m.Snapshot()

	// Record more
	m.RecordRequest(200 * time.Millisecond)

	// First snapshot should be unchanged
	assert.Equal(t, 1, snapshot1["requests_total"])

	// New snapshot should reflect new data
	snapshot2 := m.Snapshot()
	assert.Equal(t, 2, snapshot2["requests_total"])
}
