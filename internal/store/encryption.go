package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

const (
	// chunkSize is the size of plaintext chunks for streaming encryption (64KB)
	chunkSize = 64 * 1024
)

// EncryptionConfig holds encryption-related configuration
type EncryptionConfig struct {
	Enabled bool   // Whether encryption is enabled
	KeyPath string // Path to the encryption key file
}

// generateEncryptionKey generates a new 32-byte (256-bit) AES key
func generateEncryptionKey() ([]byte, error) {
	key := make([]byte, 32) // AES-256
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate encryption key: %w", err)
	}
	return key, nil
}

// loadOrCreateEncryptionKey loads an existing key from file or creates a new one
func loadOrCreateEncryptionKey(keyPath string) ([]byte, error) {
	// Try to load existing key
	if data, err := os.ReadFile(keyPath); err == nil {
		if len(data) == 32 {
			log.Printf("loaded existing encryption key from %s", keyPath)
			return data, nil
		}
		return nil, fmt.Errorf("invalid key file: expected 32 bytes, got %d", len(data))
	}

	// Generate new key
	key, err := generateEncryptionKey()
	if err != nil {
		return nil, err
	}

	// Ensure directory exists
	dir := filepath.Dir(keyPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create key directory: %w", err)
	}

	// Save key to file with restricted permissions
	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		return nil, fmt.Errorf("failed to save encryption key: %w", err)
	}

	log.Printf("generated and saved new encryption key to %s", keyPath)
	return key, nil
}

// encryptingWriter wraps a writer and encrypts data in chunks as it's written
type encryptingWriter struct {
	dst    io.Writer
	gcm    cipher.AEAD
	buf    []byte
	closed bool
}

func newEncryptingWriter(key []byte, dst io.Writer) (*encryptingWriter, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	return &encryptingWriter{
		dst: dst,
		gcm: gcm,
		buf: make([]byte, 0, chunkSize),
	}, nil
}

func (w *encryptingWriter) Write(p []byte) (n int, err error) {
	if w.closed {
		return 0, fmt.Errorf("writer is closed")
	}

	n = len(p)
	w.buf = append(w.buf, p...)

	// Write complete chunks
	for len(w.buf) >= chunkSize {
		if err := w.writeChunk(w.buf[:chunkSize]); err != nil {
			return 0, err
		}
		w.buf = w.buf[chunkSize:]
	}

	return n, nil
}

func (w *encryptingWriter) writeChunk(chunk []byte) error {
	nonce := make([]byte, w.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt chunk
	ciphertext := w.gcm.Seal(nonce, nonce, chunk, nil)

	// Write chunk length (4 bytes) + encrypted data
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(ciphertext)))

	if _, err := w.dst.Write(lenBuf); err != nil {
		return err
	}
	if _, err := w.dst.Write(ciphertext); err != nil {
		return err
	}

	return nil
}

// Close flushes any remaining data
func (w *encryptingWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true

	// Write remaining data as final chunk (even if empty, to mark end)
	if len(w.buf) > 0 {
		return w.writeChunk(w.buf)
	}
	return nil
}

// decryptingReader wraps a reader and decrypts data in chunks as it's read
type decryptingReader struct {
	src       io.Reader
	gcm       cipher.AEAD
	buf       []byte
	readPos   int
	exhausted bool
}

func newDecryptingReader(key []byte, src io.Reader) (*decryptingReader, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	return &decryptingReader{
		src: src,
		gcm: gcm,
	}, nil
}

func (r *decryptingReader) Read(p []byte) (n int, err error) {
	// If we have buffered decrypted data, return it
	if r.readPos < len(r.buf) {
		n = copy(p, r.buf[r.readPos:])
		r.readPos += n
		return n, nil
	}

	// If exhausted, return EOF
	if r.exhausted {
		return 0, io.EOF
	}

	// Read next chunk
	if err := r.readNextChunk(); err != nil {
		if err == io.EOF {
			r.exhausted = true
		}
		return 0, err
	}

	// Return data from newly read chunk
	n = copy(p, r.buf[r.readPos:])
	r.readPos += n
	return n, nil
}

func (r *decryptingReader) readNextChunk() error {
	// Read chunk length
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(r.src, lenBuf); err != nil {
		return err
	}

	chunkLen := binary.BigEndian.Uint32(lenBuf)
	if chunkLen > chunkSize+uint32(r.gcm.NonceSize())+uint32(r.gcm.Overhead())+1024 {
		return fmt.Errorf("chunk too large: %d", chunkLen)
	}

	// Read encrypted chunk
	ciphertext := make([]byte, chunkLen)
	if _, err := io.ReadFull(r.src, ciphertext); err != nil {
		return err
	}

	// Decrypt
	nonceSize := r.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := r.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return fmt.Errorf("failed to decrypt chunk: %w", err)
	}

	r.buf = plaintext
	r.readPos = 0
	return nil
}
