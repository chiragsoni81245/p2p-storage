package store

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/chiragsoni81245/p2p-storage/internal/observability"
)

type PathKey struct {
	Path     string
	Filename string
}

func (p *PathKey) GetFilePath() string {
	return fmt.Sprintf("%s/%s", p.Path, p.Filename)
}

// GenerateFileKey creates a deterministic key from file content using SHA256
func GenerateFileKey(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("failed to hash file: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// GenerateKeyFromReader creates a deterministic key from reader content using SHA256
func GenerateKeyFromReader(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", fmt.Errorf("failed to hash content: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func CASPathTransformFunc(key string) PathKey {
	hash := sha1.Sum([]byte(key))
	hashStr := hex.EncodeToString(hash[:])

	blockSize := 5
	sliceLen := len(hashStr) / blockSize

	paths := make([]string, sliceLen)

	for i := 0; i < sliceLen; i++ {
		from, to := i*blockSize, (i*blockSize)+blockSize
		paths[i] = hashStr[from:to]
	}

	return PathKey{
		Path:     strings.Join(paths, "/"),
		Filename: hashStr,
	}
}

type PathTransformFunc func(string) PathKey

func DefaultPathTranformFunc(key string) PathKey {
	return PathKey{
		Path:     key,
		Filename: key,
	}
}

type StoreOpts struct {
	// Root is the path of the folder which will contain all the files and directories of that store
	Root              string
	PathTransformFunc PathTransformFunc
	// Encryption configuration (optional)
	Encryption EncryptionConfig
	// Logger (optional - uses default if not provided)
	Logger *observability.Logger
}

type Store struct {
	StoreOpts
	encryptionKey []byte // nil if encryption is disabled
	logger        *observability.Logger
}

func NewStore(opts StoreOpts) (*Store, error) {
	if opts.PathTransformFunc == nil {
		opts.PathTransformFunc = DefaultPathTranformFunc
	}

	logger := opts.Logger
	if logger == nil {
		logger = observability.NewLogger(observability.Fields{"service": "store"})
	}

	s := &Store{
		StoreOpts: opts,
		logger:    logger,
	}

	// Initialize encryption if enabled
	if opts.Encryption.Enabled {
		keyPath := opts.Encryption.KeyPath
		if keyPath == "" {
			// Default key path relative to storage root
			keyPath = filepath.Join(opts.Root, ".encryption_key")
		}

		key, err := loadOrCreateEncryptionKey(keyPath)
		if err != nil {
			return nil, fmt.Errorf("encryption enabled but failed to initialize: %w", err)
		}
		s.encryptionKey = key
		s.logger.Info("encryption enabled for store", observability.Fields{"root": opts.Root})
	}

	return s, nil
}

// IsEncryptionEnabled returns whether encryption is active for this store
func (s *Store) IsEncryptionEnabled() bool {
	return s.encryptionKey != nil
}

func (s *Store) getAbsolutePath(path string) string {
	return fmt.Sprintf("%s/%s", s.Root, path)
}

func (s *Store) Clear() error {
	return os.RemoveAll(s.Root)
}

func (s *Store) Has(key string) bool {
	pathKey := s.PathTransformFunc(key)
	_, err := os.Stat(s.getAbsolutePath(pathKey.GetFilePath()))
	return !os.IsNotExist(err)
}

func (s *Store) Delete(key string) error {
	pathKey := s.PathTransformFunc(key)
	defer func() {
		s.logger.Debug("deleted file from disk", observability.Fields{"path": s.getAbsolutePath(pathKey.GetFilePath())})
	}()

	err := os.Remove(s.getAbsolutePath(pathKey.GetFilePath()))
	if err != nil {
		return err
	}

	subFolders := strings.Split(s.getAbsolutePath(pathKey.Path), "/")
	for i := len(subFolders) - 1; i >= 0; i-- {
		subPath := strings.Join(subFolders[:i+1], "/")
		dirEntries, err := os.ReadDir(subPath)
		if len(dirEntries) == 0 {
			err = os.Remove(subPath)
		} else {
			break
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) Read(key string) (io.Reader, int64, error) {
	if !s.Has(key) {
		return nil, 0, fmt.Errorf("file not found")
	}
	f, fi, err := s.readStream(key)
	if err != nil {
		return nil, 0, err
	}

	// If encryption is enabled, use streaming decryption
	if s.IsEncryptionEnabled() {
		// We need to read into a buffer because the decrypting reader
		// needs the file to stay open, and we're returning a reader.
		// Use streaming decryption to avoid loading entire ciphertext at once.
		decReader, err := newDecryptingReader(s.encryptionKey, f)
		if err != nil {
			f.Close()
			return nil, 0, fmt.Errorf("failed to create decrypting reader: %w", err)
		}

		// Read decrypted content into buffer (streaming chunk by chunk)
		buf := new(bytes.Buffer)
		n, err := io.Copy(buf, decReader)
		f.Close()
		if err != nil {
			return nil, 0, fmt.Errorf("failed to decrypt file: %w", err)
		}
		return buf, n, nil
	}

	// Non-encrypted: read into buffer
	defer f.Close()
	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, f)
	if err != nil {
		return nil, 0, err
	}

	return buf, (*fi).Size(), nil
}

func (s *Store) Write(key string, r io.Reader) (size int64, err error) {
	// If encryption is enabled, use streaming encryption
	if s.IsEncryptionEnabled() {
		return s.writeStreamEncrypted(key, r)
	}

	return s.writeStream(key, r)
}

// ReadRaw reads the raw bytes from storage without decryption.
// Use this for network transfers where the encrypted content should be sent as-is.
func (s *Store) ReadRaw(key string) (io.Reader, int64, error) {
	if !s.Has(key) {
		return nil, 0, fmt.Errorf("file not found")
	}
	f, fi, err := s.readStream(key)
	if err != nil {
		return nil, 0, err
	}

	// Read raw bytes into buffer (no decryption)
	defer f.Close()
	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, f)
	if err != nil {
		return nil, 0, err
	}

	return buf, (*fi).Size(), nil
}

// WriteRaw writes raw bytes to storage without encryption.
// Use this for storing files received from network that are already encrypted.
func (s *Store) WriteRaw(key string, r io.Reader) (size int64, err error) {
	return s.writeStream(key, r)
}

// writeStreamEncrypted writes data with streaming encryption
func (s *Store) writeStreamEncrypted(key string, r io.Reader) (int64, error) {
	pathKey := s.PathTransformFunc(key)

	if err := os.MkdirAll(s.getAbsolutePath(pathKey.Path), os.ModePerm); err != nil {
		return 0, err
	}

	filePath := s.getAbsolutePath(pathKey.GetFilePath())
	f, err := os.Create(filePath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	// Create encrypting writer
	encWriter, err := newEncryptingWriter(s.encryptionKey, f)
	if err != nil {
		return 0, fmt.Errorf("failed to create encrypting writer: %w", err)
	}

	// Stream data through encrypting writer
	n, err := io.Copy(encWriter, r)
	if err != nil {
		return 0, fmt.Errorf("failed to write encrypted data: %w", err)
	}

	// Flush remaining data
	if err := encWriter.Close(); err != nil {
		return 0, fmt.Errorf("failed to finalize encryption: %w", err)
	}

	s.logger.Debug("written encrypted bytes to disk", observability.Fields{"bytes": n, "path": filePath})
	return n, nil
}

func (s *Store) readStream(key string) (*os.File, *os.FileInfo, error) {
	pathKey := s.PathTransformFunc(key)
	filepath := s.getAbsolutePath(pathKey.GetFilePath())
	fi, err := os.Stat(filepath)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(filepath)

	return f, &fi, err
}

func (s *Store) writeStream(key string, r io.Reader) (int64, error) {
	pathKey := s.PathTransformFunc(key)

	if err := os.MkdirAll(s.getAbsolutePath(pathKey.Path), os.ModePerm); err != nil {
		return 0, err
	}

	filepath := s.getAbsolutePath(pathKey.GetFilePath())
	f, err := os.Create(filepath)
	if err != nil {
		return 0, err
	}

	n, err := io.Copy(f, r)
	if err != nil {
		return 0, err
	}

	s.logger.Debug("written bytes to disk", observability.Fields{"bytes": n, "path": filepath})

	return n, nil
}
