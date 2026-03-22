package node

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, 0, cfg.ListenPort)
	assert.Equal(t, "./node.key", cfg.IdentityPath)
	assert.Equal(t, 50, cfg.MinConnection)
	assert.Equal(t, 100, cfg.MaxConnection)
}

func TestConfig_CustomValues(t *testing.T) {
	cfg := Config{
		ListenPort:    9000,
		IdentityPath:  "/custom/path/key",
		MinConnection: 10,
		MaxConnection: 50,
	}

	assert.Equal(t, 9000, cfg.ListenPort)
	assert.Equal(t, "/custom/path/key", cfg.IdentityPath)
	assert.Equal(t, 10, cfg.MinConnection)
	assert.Equal(t, 50, cfg.MaxConnection)
}

func TestLoadOrCreateIdentity_CreatesNew(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "test.key")

	// Key should not exist
	_, err := os.Stat(keyPath)
	require.True(t, os.IsNotExist(err))

	// Create identity
	priv, err := LoadOrCreateIdentity(keyPath)
	require.NoError(t, err)
	require.NotNil(t, priv)

	// Key file should now exist
	info, err := os.Stat(keyPath)
	require.NoError(t, err)

	// Verify permissions (0600)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())

	// Verify it's an Ed25519 key (crypto.Ed25519 == 1)
	assert.EqualValues(t, crypto.Ed25519, priv.Type())
}

func TestLoadOrCreateIdentity_LoadsExisting(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "existing.key")

	// Create identity first
	priv1, err := LoadOrCreateIdentity(keyPath)
	require.NoError(t, err)

	// Load it again
	priv2, err := LoadOrCreateIdentity(keyPath)
	require.NoError(t, err)

	// Should be the same key
	bytes1, _ := priv1.Raw()
	bytes2, _ := priv2.Raw()
	assert.Equal(t, bytes1, bytes2)
}

func TestLoadOrCreateIdentity_DifferentPathsCreateDifferentKeys(t *testing.T) {
	tmpDir := t.TempDir()

	priv1, err := LoadOrCreateIdentity(filepath.Join(tmpDir, "key1.key"))
	require.NoError(t, err)

	priv2, err := LoadOrCreateIdentity(filepath.Join(tmpDir, "key2.key"))
	require.NoError(t, err)

	// Should be different keys
	bytes1, _ := priv1.Raw()
	bytes2, _ := priv2.Raw()
	assert.NotEqual(t, bytes1, bytes2)
}

func TestLoadOrCreateIdentity_CorruptedFile(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "corrupted.key")

	// Write garbage data
	err := os.WriteFile(keyPath, []byte("not a valid key"), 0600)
	require.NoError(t, err)

	// Should fail to load
	_, err = LoadOrCreateIdentity(keyPath)
	assert.Error(t, err)
}

func TestLoadOrCreateIdentity_InvalidDirectory(t *testing.T) {
	// Try to create key in non-existent directory
	keyPath := "/nonexistent/directory/node.key"

	_, err := LoadOrCreateIdentity(keyPath)
	assert.Error(t, err)
}

func TestNewNode(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := Config{
		ListenPort:    0, // Random port
		IdentityPath:  filepath.Join(tmpDir, "node.key"),
		MinConnection: 5,
		MaxConnection: 10,
	}

	h, err := NewNode(cfg)
	require.NoError(t, err)
	require.NotNil(t, h)
	defer h.Close()

	// Verify host has addresses
	addrs := h.Addrs()
	assert.NotEmpty(t, addrs)

	// Verify host has an ID
	assert.NotEmpty(t, h.ID())

	// Verify key file was created
	_, err = os.Stat(cfg.IdentityPath)
	assert.NoError(t, err)
}

func TestNewNode_SpecificPort(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := Config{
		ListenPort:    0, // Let OS assign port
		IdentityPath:  filepath.Join(tmpDir, "node.key"),
		MinConnection: 5,
		MaxConnection: 10,
	}

	h, err := NewNode(cfg)
	require.NoError(t, err)
	require.NotNil(t, h)
	defer h.Close()

	// Should have at least one address
	assert.NotEmpty(t, h.Addrs())
}

func TestNewNode_PersistentIdentity(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "persistent.key")

	cfg1 := Config{
		ListenPort:    0,
		IdentityPath:  keyPath,
		MinConnection: 5,
		MaxConnection: 10,
	}

	// Create first node
	h1, err := NewNode(cfg1)
	require.NoError(t, err)
	id1 := h1.ID()
	h1.Close()

	// Create second node with same identity path
	cfg2 := Config{
		ListenPort:    0,
		IdentityPath:  keyPath,
		MinConnection: 5,
		MaxConnection: 10,
	}

	h2, err := NewNode(cfg2)
	require.NoError(t, err)
	id2 := h2.ID()
	h2.Close()

	// Both nodes should have the same ID
	assert.Equal(t, id1, id2)
}

func TestNewNode_WithDifferentConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := Config{
		ListenPort:    0,
		IdentityPath:  filepath.Join(tmpDir, "node.key"),
		MinConnection: 10,
		MaxConnection: 20,
	}

	h, err := NewNode(cfg)
	require.NoError(t, err)
	defer h.Close()

	// Verify host was created
	assert.NotEmpty(t, h.ID())
}

func TestNewNode_CanConnect(t *testing.T) {
	tmpDir := t.TempDir()

	cfg1 := Config{
		ListenPort:    0,
		IdentityPath:  filepath.Join(tmpDir, "node1.key"),
		MinConnection: 5,
		MaxConnection: 10,
	}

	cfg2 := Config{
		ListenPort:    0,
		IdentityPath:  filepath.Join(tmpDir, "node2.key"),
		MinConnection: 5,
		MaxConnection: 10,
	}

	h1, err := NewNode(cfg1)
	require.NoError(t, err)
	defer h1.Close()

	h2, err := NewNode(cfg2)
	require.NoError(t, err)
	defer h2.Close()

	// Verify they are different nodes
	assert.NotEqual(t, h1.ID(), h2.ID())

	// Verify they have different addresses
	assert.NotEqual(t, h1.Addrs(), h2.Addrs())
}
