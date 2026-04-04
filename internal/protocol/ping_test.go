//go:build unit

package protocol

import (
	"context"
	"testing"

	"github.com/chiragsoni81245/p2p-storage/internal/observability"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPingHandler_Handle_Success(t *testing.T) {
	logger := observability.NewLogger(observability.Fields{})
	handler := &PingHandler{Logger: logger}

	msg := NewMessage("PING", "hello")

	resp, err := handler.Handle(context.Background(), peer.ID("test-peer"), msg)
	require.NoError(t, err)

	respMsg, ok := resp.(Message)
	require.True(t, ok)
	assert.Equal(t, "PONG", respMsg.Type)

	data, err := Decode[string](respMsg)
	require.NoError(t, err)
	assert.Equal(t, "hello back", data)
}

func TestPingHandler_Handle_InvalidMessageType_Unit(t *testing.T) {
	logger := observability.NewLogger(observability.Fields{})
	handler := &PingHandler{Logger: logger}

	_, err := handler.Handle(context.Background(), peer.ID("test-peer"), "invalid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid message type")
}
