package operations

import (
	"fmt"
	"os"

	"github.com/chiragsoni81245/p2p-storage/internal/store"
)

// GetFileKey computes and returns the content-addressed key for a file.
// It is a pure function: no FileServer or network involvement.
func GetFileKey(filePath string) (string, error) {
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("file does not exist: %s", filePath)
	}
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("cannot get key for a directory: %s", filePath)
	}

	return store.GenerateFileKey(filePath)
}
