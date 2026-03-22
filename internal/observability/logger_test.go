package observability

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLogger(t *testing.T) {
	logger := NewLogger(Fields{"service": "test"})
	require.NotNil(t, logger)
	assert.Equal(t, Fields{"service": "test"}, logger.fields)
}

func TestNewLoggerWithWriter(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewLoggerWithWriter(buf, Fields{"service": "test"})

	require.NotNil(t, logger)
	assert.Equal(t, buf, logger.out)
}

func TestLogger_Info(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewLoggerWithWriter(buf, Fields{"service": "test-service"})

	logger.Info("test message", Fields{"key": "value"})

	var entry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err)

	assert.Equal(t, "INFO", entry["level"])
	assert.Equal(t, "test message", entry["msg"])
	assert.Equal(t, "test-service", entry["service"])
	assert.Equal(t, "value", entry["key"])
	assert.NotEmpty(t, entry["ts"])
}

func TestLogger_Error(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewLoggerWithWriter(buf, Fields{})

	logger.Error("error occurred", Fields{"error": "something went wrong"})

	var entry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err)

	assert.Equal(t, "ERROR", entry["level"])
	assert.Equal(t, "error occurred", entry["msg"])
	assert.Equal(t, "something went wrong", entry["error"])
}

func TestLogger_Debug(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewLoggerWithWriter(buf, Fields{})

	logger.Debug("debug info", Fields{"debug_data": 123})

	var entry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err)

	assert.Equal(t, "DEBUG", entry["level"])
	assert.Equal(t, "debug info", entry["msg"])
	assert.Equal(t, float64(123), entry["debug_data"]) // JSON numbers are float64
}

func TestLogger_StaticFields(t *testing.T) {
	buf := &bytes.Buffer{}
	staticFields := Fields{
		"service": "my-service",
		"version": "1.0.0",
		"node_id": "node-123",
	}
	logger := NewLoggerWithWriter(buf, staticFields)

	logger.Info("test", Fields{})

	var entry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err)

	assert.Equal(t, "my-service", entry["service"])
	assert.Equal(t, "1.0.0", entry["version"])
	assert.Equal(t, "node-123", entry["node_id"])
}

func TestLogger_DynamicFieldsOverrideStatic(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewLoggerWithWriter(buf, Fields{"key": "static"})

	logger.Info("test", Fields{"key": "dynamic"})

	var entry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err)

	// Dynamic should override static
	assert.Equal(t, "dynamic", entry["key"])
}

func TestLogger_TimestampFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewLoggerWithWriter(buf, Fields{})

	logger.Info("test", Fields{})

	var entry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err)

	ts, ok := entry["ts"].(string)
	require.True(t, ok)

	// Should be RFC3339 format
	_, err = time.Parse(time.RFC3339, ts)
	assert.NoError(t, err)
}

func TestLogger_NewlineAfterEntry(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewLoggerWithWriter(buf, Fields{})

	logger.Info("test1", Fields{})
	logger.Info("test2", Fields{})

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	assert.Len(t, lines, 2)
}

func TestLogger_NilFields(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewLoggerWithWriter(buf, nil)

	// Should not panic with nil fields
	assert.NotPanics(t, func() {
		logger.Info("test", nil)
	})

	var entry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err)
	assert.Equal(t, "test", entry["msg"])
}

func TestLogger_ConcurrentWrites(t *testing.T) {
	buf := &safeBuffer{}
	logger := NewLoggerWithWriter(buf, Fields{})

	var wg sync.WaitGroup
	numGoroutines := 10
	numLogs := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < numLogs; j++ {
				logger.Info("concurrent log", Fields{"goroutine": idx, "iteration": j})
			}
		}(i)
	}

	wg.Wait()

	// Count log entries
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	assert.Equal(t, numGoroutines*numLogs, len(lines))
}

// safeBuffer is a thread-safe bytes.Buffer
type safeBuffer struct {
	buf bytes.Buffer
	mu  sync.Mutex
}

func (s *safeBuffer) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}
