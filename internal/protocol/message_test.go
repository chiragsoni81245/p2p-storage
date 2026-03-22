//go:build unit

package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMessageConstants(t *testing.T) {
	// Test message type constants
	assert.Equal(t, "STORE_FILE", TypeStoreFile)
	assert.Equal(t, "STORE_FILE_ACK", TypeStoreFileAck)
	assert.Equal(t, "GET_FILE", TypeGetFile)
	assert.Equal(t, "GET_FILE_RESPONSE", TypeGetFileResp)
	assert.Equal(t, "ERROR", TypeError)
}

func TestResponseDataConstants(t *testing.T) {
	// Test response data constants
	assert.Equal(t, "success", DataSuccess)
	assert.Equal(t, "found", DataFound)
	assert.Equal(t, "file not found", DataNotFound)
	assert.Equal(t, "failed to read file", DataReadFailed)
	assert.Equal(t, "key cannot be empty", DataEmptyKey)
	assert.Equal(t, "peer not registered", DataPeerNotFound)
	assert.Equal(t, "invalid message format", DataInvalidMsg)
	assert.Equal(t, "unknown message type", DataUnknownType)
}

func TestMessageStruct(t *testing.T) {
	msg := Message{
		Type: TypeStoreFile,
		Key:  "test-key",
		Data: "test-data",
	}

	assert.Equal(t, TypeStoreFile, msg.Type)
	assert.Equal(t, "test-key", msg.Key)
	assert.Equal(t, "test-data", msg.Data)
}
