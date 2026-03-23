package observability

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"
)

// LogLevel represents the severity level of log messages
type LogLevel int

const (
	// LevelDebug logs all messages including debug
	LevelDebug LogLevel = iota
	// LevelInfo logs info, warn, and error messages
	LevelInfo
	// LevelError logs only error messages
	LevelError
)

// String returns the string representation of the log level
func (l LogLevel) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelError:
		return "error"
	default:
		return "info"
	}
}

// ParseLogLevel parses a string into a LogLevel
func ParseLogLevel(s string) LogLevel {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

type Fields map[string]any

type Logger struct {
	out    io.Writer
	fields Fields   // static fields
	level  LogLevel // minimum log level
}

func NewLogger(fields Fields) *Logger {
	return &Logger{
		out:    os.Stdout,
		fields: fields,
		level:  LevelInfo,
	}
}

// NewLoggerWithWriter creates a logger with a custom writer (for testing)
func NewLoggerWithWriter(w io.Writer, fields Fields) *Logger {
	return &Logger{
		out:    w,
		fields: fields,
		level:  LevelInfo,
	}
}

// NewLoggerWithLevel creates a logger with a custom writer and log level
func NewLoggerWithLevel(w io.Writer, fields Fields, level LogLevel) *Logger {
	return &Logger{
		out:    w,
		fields: fields,
		level:  level,
	}
}

// SetLevel sets the minimum log level
func (l *Logger) SetLevel(level LogLevel) {
	l.level = level
}

// GetLevel returns the current log level
func (l *Logger) GetLevel() LogLevel {
	return l.level
}

func (l *Logger) log(level string, minLevel LogLevel, msg string, fields Fields) {
	// Skip if message level is below minimum
	if minLevel < l.level {
		return
	}

	entry := make(map[string]interface{})

	entry["ts"] = time.Now().UTC().Format(time.RFC3339)
	entry["level"] = level
	entry["msg"] = msg

	// static fields (service, node_id, etc.)
	for k, v := range l.fields {
		entry[k] = v
	}

	// dynamic fields
	for k, v := range fields {
		entry[k] = v
	}

	data, _ := json.Marshal(entry)

	l.out.Write(data)
	l.out.Write([]byte("\n"))
}

func (l *Logger) Info(msg string, fields Fields) {
	l.log("INFO", LevelInfo, msg, fields)
}

func (l *Logger) Error(msg string, fields Fields) {
	l.log("ERROR", LevelError, msg, fields)
}

func (l *Logger) Debug(msg string, fields Fields) {
	l.log("DEBUG", LevelDebug, msg, fields)
}
