package observability

import (
	"encoding/json"
	"os"
	"time"
)

type Fields map[string]any

type Logger struct {
	out    *os.File
	fields Fields // static fields
}

func NewLogger(fields Fields) *Logger {
	return &Logger{
		out: os.Stdout,
		fields: fields,
	}
}

func (l *Logger) log(level string, msg string, fields Fields) {
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
	l.log("INFO", msg, fields)
}

func (l *Logger) Error(msg string, fields Fields) {
	l.log("ERROR", msg, fields)
}

func (l *Logger) Debug(msg string, fields Fields) {
	l.log("DEBUG", msg, fields)
}
