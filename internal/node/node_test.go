//go:build integration

package node

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
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

	_, err := os.Stat(keyPath)
	require.True(t, os.IsNotExist(err))

	priv, err := LoadOrCreateIdentity(keyPath)
	require.NoError(t, err)
	require.NotNil(t, priv)

	info, err := os.Stat(keyPath)
	require.NoError(t, err)

	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	assert.EqualValues(t, crypto.Ed25519, priv.Type())
}

func TestLoadOrCreateIdentity_LoadsExisting(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "existing.key")

	priv1, err := LoadOrCreateIdentity(keyPath)
	require.NoError(t, err)

	priv2, err := LoadOrCreateIdentity(keyPath)
	require.NoError(t, err)

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

	bytes1, _ := priv1.Raw()
	bytes2, _ := priv2.Raw()
	assert.NotEqual(t, bytes1, bytes2)
}

func TestLoadOrCreateIdentity_CorruptedFile(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "corrupted.key")

	err := os.WriteFile(keyPath, []byte("not a valid key"), 0600)
	require.NoError(t, err)

	_, err = LoadOrCreateIdentity(keyPath)
	assert.Error(t, err)
}

func TestLoadOrCreateIdentity_InvalidDirectory(t *testing.T) {
	keyPath := "/nonexistent/directory/node.key"

	_, err := LoadOrCreateIdentity(keyPath)
	assert.Error(t, err)
}

func TestNewNode(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := Config{
		ListenPort:    0,
		IdentityPath:  filepath.Join(tmpDir, "node.key"),
		MinConnection: 5,
		MaxConnection: 10,
	}

	h, err := NewNode(cfg)
	require.NoError(t, err)
	require.NotNil(t, h)
	defer h.Close()

	assert.NotEmpty(t, h.Addrs())
	assert.NotEmpty(t, h.ID())

	_, err = os.Stat(cfg.IdentityPath)
	assert.NoError(t, err)
}

func TestNewNode_PersistentIdentity(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "persistent.key")

	cfg := Config{
		ListenPort:    0,
		IdentityPath:  keyPath,
		MinConnection: 5,
		MaxConnection: 10,
	}

	h1, err := NewNode(cfg)
	require.NoError(t, err)
	id1 := h1.ID()
	h1.Close()

	h2, err := NewNode(cfg)
	require.NoError(t, err)
	id2 := h2.ID()
	h2.Close()

	assert.Equal(t, id1, id2)
}

func TestNewNode_CanConnect(t *testing.T) {
	tmpDir := t.TempDir()

	h1, err := NewNode(Config{
		ListenPort:    0,
		IdentityPath:  filepath.Join(tmpDir, "node1.key"),
		MinConnection: 5,
		MaxConnection: 10,
	})
	require.NoError(t, err)
	defer h1.Close()

	h2, err := NewNode(Config{
		ListenPort:    0,
		IdentityPath:  filepath.Join(tmpDir, "node2.key"),
		MinConnection: 5,
		MaxConnection: 10,
	})
	require.NoError(t, err)
	defer h2.Close()

	assert.NotEqual(t, h1.ID(), h2.ID())

	err = h1.Connect(context.Background(), peer.AddrInfo{ID: h2.ID(), Addrs: h2.Addrs()})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return h1.Network().Connectedness(h2.ID()) == network.Connected
	}, 5*time.Second, 100*time.Millisecond)
}
