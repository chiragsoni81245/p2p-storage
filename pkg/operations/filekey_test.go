//go:build unit

package operations

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetFileKey_ReturnsHex(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "testfile")
	require.NoError(t, err)
	_, err = f.WriteString("hello world")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	key, err := GetFileKey(f.Name())
	require.NoError(t, err)
	assert.Len(t, key, 64) // SHA256 hex = 64 chars
}

func TestGetFileKey_Deterministic(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "testfile")
	require.NoError(t, err)
	_, err = f.WriteString("consistent content")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	key1, err := GetFileKey(f.Name())
	require.NoError(t, err)

	key2, err := GetFileKey(f.Name())
	require.NoError(t, err)

	assert.Equal(t, key1, key2)
}

func TestGetFileKey_DifferentContentDifferentKey(t *testing.T) {
	dir := t.TempDir()

	f1, err := os.CreateTemp(dir, "file1")
	require.NoError(t, err)
	_, err = f1.WriteString("content A")
	require.NoError(t, err)
	require.NoError(t, f1.Close())

	f2, err := os.CreateTemp(dir, "file2")
	require.NoError(t, err)
	_, err = f2.WriteString("content B")
	require.NoError(t, err)
	require.NoError(t, f2.Close())

	key1, err := GetFileKey(f1.Name())
	require.NoError(t, err)

	key2, err := GetFileKey(f2.Name())
	require.NoError(t, err)

	assert.NotEqual(t, key1, key2)
}

func TestGetFileKey_FileNotExist(t *testing.T) {
	_, err := GetFileKey("/nonexistent/path/file.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestGetFileKey_Directory(t *testing.T) {
	_, err := GetFileKey(t.TempDir())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "directory")
}

func TestGetFileKey_EmptyFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "empty")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	key, err := GetFileKey(f.Name())
	require.NoError(t, err)
	assert.Len(t, key, 64)
}

func TestGetFileKey_RelativePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rel.txt")
	require.NoError(t, os.WriteFile(path, []byte("data"), 0644))

	key, err := GetFileKey(path)
	require.NoError(t, err)
	assert.Len(t, key, 64)
}
