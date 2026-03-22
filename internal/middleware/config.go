package middleware

import "time"

type RateLimiterConfig struct {
	Limit  int
	Window time.Duration
	TTL    time.Duration
}

func DefaultRateLimiterConfig() RateLimiterConfig {
	return RateLimiterConfig{
		Limit:  100, // Default max concurrent requests
		Window: time.Second,
		TTL:    5 * time.Minute,
	}
}
