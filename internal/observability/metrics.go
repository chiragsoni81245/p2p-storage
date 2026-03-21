package observability

import (
	"sync"
	"time"
)

type Metrics struct {
	mu sync.Mutex

	RequestsTotal int
	FailuresTotal int

	TotalLatency time.Duration
	RequestCount int
}

func NewMetrics() *Metrics {
	return &Metrics{}
}

func (m *Metrics) RecordRequest(latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.RequestsTotal++
	m.TotalLatency += latency
	m.RequestCount++
}

func (m *Metrics) RecordFailure() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.FailuresTotal++
}

func (m *Metrics) Snapshot() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()

	avgLatency := time.Duration(0)
	if m.RequestCount > 0 {
		avgLatency = m.TotalLatency / time.Duration(m.RequestCount)
	}

	return map[string]any{
		"requests_total": m.RequestsTotal,
		"failures_total": m.FailuresTotal,
		"avg_latency":    avgLatency,
	}
}
