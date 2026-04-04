//go:build unit

package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageTypeConstants(t *testing.T) {
	assert.Equal(t, "STORE_FILE", TypeStoreFile)
	assert.Equal(t, "STORE_FILE_RESP", TypeStoreFileResp)
	assert.Equal(t, "GET_FILE", TypeGetFile)
	assert.Equal(t, "ERROR", TypeError)
}

func TestStatusConstants(t *testing.T) {
	assert.Equal(t, "success", StatusSuccess)
	assert.Equal(t, "file not found", StatusNotFound)
	assert.Equal(t, "failed to read file", StatusReadFailed)
	assert.Equal(t, "key cannot be empty", StatusEmptyKey)
	assert.Equal(t, "invalid message format", StatusInvalidMsg)
	assert.Equal(t, "unknown message type", StatusUnknownType)
	assert.Equal(t, "session rejected", StatusSessionRejected)
	assert.Equal(t, "file already exists", StatusAlreadyExists)
}

func TestNewMessage(t *testing.T) {
	msg := NewMessage(TypeStoreFile, StoreFilePayload{Key: "abc", Session: "s"})
	assert.Equal(t, TypeStoreFile, msg.Type)
	assert.NotNil(t, msg.Data)

	var p StoreFilePayload
	require.NoError(t, json.Unmarshal(msg.Data, &p))
	assert.Equal(t, "abc", p.Key)
	assert.Equal(t, "s", p.Session)
}

func TestNewMessage_PanicsOnUnmarshallable(t *testing.T) {
	assert.Panics(t, func() {
		NewMessage(TypeStoreFile, make(chan int))
	})
}

func TestDecode_StoreFilePayload(t *testing.T) {
	msg := NewMessage(TypeStoreFile, StoreFilePayload{Key: "testkey", Session: "sess"})
	p, err := Decode[StoreFilePayload](msg)
	require.NoError(t, err)
	assert.Equal(t, "testkey", p.Key)
	assert.Equal(t, "sess", p.Session)
}

func TestDecode_StoreFileRespPayload(t *testing.T) {
	msg := NewMessage(TypeStoreFileResp, StoreFileRespPayload{Key: "k", Status: StatusSuccess})
	p, err := Decode[StoreFileRespPayload](msg)
	require.NoError(t, err)
	assert.Equal(t, "k", p.Key)
	assert.Equal(t, StatusSuccess, p.Status)
}

func TestDecode_GetFilePayload(t *testing.T) {
	msg := NewMessage(TypeGetFile, GetFilePayload{
		Key: "k", MsgID: "m", TTL: 3,
		RequesterID:    "peer1",
		RequesterAddrs: []string{"/ip4/1.2.3.4/tcp/4001"},
	})
	p, err := Decode[GetFilePayload](msg)
	require.NoError(t, err)
	assert.Equal(t, "k", p.Key)
	assert.Equal(t, "m", p.MsgID)
	assert.Equal(t, 3, p.TTL)
	assert.Equal(t, "peer1", p.RequesterID)
	assert.Equal(t, []string{"/ip4/1.2.3.4/tcp/4001"}, p.RequesterAddrs)
}

func TestDecode_ErrorPayload(t *testing.T) {
	msg := NewMessage(TypeError, ErrorPayload{Reason: "something went wrong"})
	p, err := Decode[ErrorPayload](msg)
	require.NoError(t, err)
	assert.Equal(t, "something went wrong", p.Reason)
}

func TestDecode_InvalidJSON(t *testing.T) {
	msg := Message{Type: TypeStoreFile, Data: json.RawMessage(`not valid json`)}
	_, err := Decode[StoreFilePayload](msg)
	assert.Error(t, err)
}

func TestMessage_JSONRoundTrip(t *testing.T) {
	original := NewMessage(TypeStoreFile, StoreFilePayload{Key: "abc"})

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded Message
	require.NoError(t, json.Unmarshal(data, &decoded))

	p, err := Decode[StoreFilePayload](decoded)
	require.NoError(t, err)
	assert.Equal(t, "abc", p.Key)
}
