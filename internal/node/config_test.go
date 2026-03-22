//go:build unit

package node

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultConfig_Unit(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, 0, cfg.ListenPort)
	assert.Equal(t, "./node.key", cfg.IdentityPath)
	assert.Equal(t, 50, cfg.MinConnection)
	assert.Equal(t, 100, cfg.MaxConnection)
	assert.Equal(t, 10, cfg.Concurrency)
}

func TestConfig_CustomValues_Unit(t *testing.T) {
	cfg := Config{
		ListenPort:    8080,
		IdentityPath:  "/custom/path/node.key",
		MinConnection: 10,
		MaxConnection: 50,
		Concurrency:   5,
	}

	assert.Equal(t, 8080, cfg.ListenPort)
	assert.Equal(t, "/custom/path/node.key", cfg.IdentityPath)
	assert.Equal(t, 10, cfg.MinConnection)
	assert.Equal(t, 50, cfg.MaxConnection)
	assert.Equal(t, 5, cfg.Concurrency)
}
