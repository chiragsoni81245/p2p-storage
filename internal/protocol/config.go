package protocol

import "time"

type Config struct {
	MaxMessageSize int64
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	HandlerTimeout time.Duration
}

func DefaultConfig() Config {
	return Config{
		MaxMessageSize: 1 << 20, // 1MB
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		HandlerTimeout: 5 * time.Second,
	}
}

