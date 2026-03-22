package store

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"
	"time"
)

func TestPathTransformFunc(t *testing.T) {
	key := "yo its a dummy string"
	pathKey := CASPathTransformFunc(key)
	filename := "90555eb6014736bedad917345c0193cd1f638ad6"
	expectedPathName := "90555/eb601/4736b/edad9/17345/c0193/cd1f6/38ad6"
	if pathKey.Filename != filename {
		t.Errorf("have filename %s want %s", pathKey.Filename, filename)
	}
	if pathKey.Path != expectedPathName {
		t.Errorf("have path name %s want %s", pathKey.Path, expectedPathName)
	}
}

func TestStore(t *testing.T) {
	store := newStore()
	defer teardown(t, store)

	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("foo_%d", i)
		data := []byte("some random bytes")
		if _, err := store.writeStream(key, bytes.NewReader(data)); err != nil {
			t.Error(err)
		}

		if !store.Has(key) {
			t.Errorf("expected to have key %s", key)
		}

		r, _, err := store.Read(key)
		if err != nil {
			t.Error(err)
		}

		b, err := io.ReadAll(r)
		if err != nil {
			t.Error(err)
		}

		if string(b) != string(data) {
			t.Errorf("want \"%s\" have \"%s\"", data, b)
		}

		if err := store.Delete(key); err != nil {
			t.Error(err)
		}

		if store.Has(key) {
			t.Errorf("not expecting the key %s", key)
		}
	}
}

func newStore() *Store {
	opts := StoreOpts{
		Root:              fmt.Sprintf("./storage/%d", time.Now().UnixMilli()),
		PathTransformFunc: CASPathTransformFunc,
	}
	s, err := NewStore(opts)
	if err != nil {
		panic(err)
	}
	return s
}

func teardown(t *testing.T, s *Store) {
	if err := s.Clear(); err != nil {
		t.Error(err)
	}
}

func TestStoreWithEncryption(t *testing.T) {
	store := newStoreWithEncryption()
	defer teardown(t, store)

	// Verify encryption is enabled
	if !store.IsEncryptionEnabled() {
		t.Error("expected encryption to be enabled")
	}

	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("encrypted_foo_%d", i)
		data := []byte("some secret data that should be encrypted")

		if _, err := store.Write(key, bytes.NewReader(data)); err != nil {
			t.Error(err)
		}

		if !store.Has(key) {
			t.Errorf("expected to have key %s", key)
		}

		r, _, err := store.Read(key)
		if err != nil {
			t.Error(err)
		}

		b, err := io.ReadAll(r)
		if err != nil {
			t.Error(err)
		}

		if string(b) != string(data) {
			t.Errorf("want \"%s\" have \"%s\"", data, b)
		}

		if err := store.Delete(key); err != nil {
			t.Error(err)
		}

		if store.Has(key) {
			t.Errorf("not expecting the key %s", key)
		}
	}
}

func TestEncryptedDataIsDifferentFromPlaintext(t *testing.T) {
	store := newStoreWithEncryption()
	defer teardown(t, store)

	key := "test_encryption_diff"
	plaintext := []byte("this is my secret plaintext data")

	// Write encrypted data
	if _, err := store.Write(key, bytes.NewReader(plaintext)); err != nil {
		t.Error(err)
	}

	// Read raw file from disk (bypassing decryption)
	pathKey := store.PathTransformFunc(key)
	rawData, err := os.ReadFile(store.getAbsolutePath(pathKey.GetFilePath()))
	if err != nil {
		t.Error(err)
	}

	// Raw data should NOT equal plaintext (it should be encrypted)
	if bytes.Equal(rawData, plaintext) {
		t.Error("encrypted data should not equal plaintext")
	}

	// But reading through store should return original plaintext
	r, _, err := store.Read(key)
	if err != nil {
		t.Error(err)
	}

	decrypted, err := io.ReadAll(r)
	if err != nil {
		t.Error(err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("decrypted data should equal plaintext, got %s", decrypted)
	}
}

func TestEncryptionKeyPersistence(t *testing.T) {
	root := fmt.Sprintf("./storage/%d_enc_persist", time.Now().UnixMilli())
	keyPath := root + "/.encryption_key"

	// Create first store
	opts := StoreOpts{
		Root:              root,
		PathTransformFunc: CASPathTransformFunc,
		Encryption: EncryptionConfig{
			Enabled: true,
			KeyPath: keyPath,
		},
	}
	store1, err := NewStore(opts)
	if err != nil {
		t.Fatalf("failed to create store1: %v", err)
	}
	defer teardown(t, store1)

	key := "persist_test"
	data := []byte("data to persist across store instances")

	// Write data with first store
	if _, err := store1.Write(key, bytes.NewReader(data)); err != nil {
		t.Error(err)
	}

	// Create second store with same key path
	store2, err := NewStore(opts)
	if err != nil {
		t.Fatalf("failed to create store2: %v", err)
	}

	// Read data with second store - should be able to decrypt
	r, _, err := store2.Read(key)
	if err != nil {
		t.Errorf("failed to read with second store: %v", err)
	}

	b, err := io.ReadAll(r)
	if err != nil {
		t.Error(err)
	}

	if string(b) != string(data) {
		t.Errorf("want \"%s\" have \"%s\"", data, b)
	}
}

func TestUnencryptedStore(t *testing.T) {
	// Test that store works without encryption (default behavior)
	store := newStore()
	defer teardown(t, store)

	if store.IsEncryptionEnabled() {
		t.Error("expected encryption to be disabled by default")
	}

	key := "unencrypted_test"
	data := []byte("unencrypted data")

	if _, err := store.Write(key, bytes.NewReader(data)); err != nil {
		t.Error(err)
	}

	r, _, err := store.Read(key)
	if err != nil {
		t.Error(err)
	}

	b, err := io.ReadAll(r)
	if err != nil {
		t.Error(err)
	}

	if string(b) != string(data) {
		t.Errorf("want \"%s\" have \"%s\"", data, b)
	}
}

func newStoreWithEncryption() *Store {
	root := fmt.Sprintf("./storage/%d_enc", time.Now().UnixMilli())
	opts := StoreOpts{
		Root:              root,
		PathTransformFunc: CASPathTransformFunc,
		Encryption: EncryptionConfig{
			Enabled: true,
			KeyPath: root + "/.encryption_key",
		},
	}
	s, err := NewStore(opts)
	if err != nil {
		panic(err)
	}
	return s
}

func TestLargeFileEncryption(t *testing.T) {
	store := newStoreWithEncryption()
	defer teardown(t, store)

	// Create a large file (256KB - spans multiple chunks of 64KB each)
	dataSize := 256 * 1024
	data := make([]byte, dataSize)
	for i := range data {
		data[i] = byte(i % 256)
	}

	key := "large_encrypted_file"

	// Write large data
	n, err := store.Write(key, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("failed to write large file: %v", err)
	}
	if n != int64(dataSize) {
		t.Errorf("expected to write %d bytes, wrote %d", dataSize, n)
	}

	// Read it back
	r, size, err := store.Read(key)
	if err != nil {
		t.Fatalf("failed to read large file: %v", err)
	}
	if size != int64(dataSize) {
		t.Errorf("expected size %d, got %d", dataSize, size)
	}

	readData, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read data: %v", err)
	}

	if !bytes.Equal(readData, data) {
		t.Error("read data does not match written data")
	}
}
