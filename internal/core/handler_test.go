package core

import (
	"context"
	"errors"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlerFunc_Handle(t *testing.T) {
	called := false
	expectedPeerID := peer.ID("test-peer")
	expectedMsg := Message("test-message")
	expectedResp := Message("response")

	handler := HandlerFunc(func(ctx context.Context, peerID peer.ID, msg Message) (Message, error) {
		called = true
		assert.Equal(t, expectedPeerID, peerID)
		assert.Equal(t, expectedMsg, msg)
		return expectedResp, nil
	})

	resp, err := handler.Handle(context.Background(), expectedPeerID, expectedMsg)

	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, expectedResp, resp)
}

func TestHandlerFunc_Handle_WithError(t *testing.T) {
	expectedErr := errors.New("handler error")

	handler := HandlerFunc(func(ctx context.Context, peerID peer.ID, msg Message) (Message, error) {
		return nil, expectedErr
	})

	resp, err := handler.Handle(context.Background(), peer.ID("test"), "msg")

	assert.Nil(t, resp)
	assert.Equal(t, expectedErr, err)
}

func TestHandlerFunc_Handle_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	handler := HandlerFunc(func(ctx context.Context, peerID peer.ID, msg Message) (Message, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			return "response", nil
		}
	})

	resp, err := handler.Handle(ctx, peer.ID("test"), "msg")

	assert.Nil(t, resp)
	assert.Equal(t, context.Canceled, err)
}

func TestHandlerFunc_ImplementsHandler(t *testing.T) {
	var _ Handler = HandlerFunc(nil)
}

func TestMessage_AnyType(t *testing.T) {
	// Message is type alias for any, should accept any type
	var msg Message

	msg = "string message"
	assert.Equal(t, "string message", msg)

	msg = 42
	assert.Equal(t, 42, msg)

	msg = map[string]string{"key": "value"}
	assert.Equal(t, map[string]string{"key": "value"}, msg)

	msg = struct{ Name string }{"test"}
	assert.Equal(t, struct{ Name string }{"test"}, msg)
}

// MockHandler implements Handler interface for testing
type MockHandler struct {
	HandleFunc func(ctx context.Context, peerID peer.ID, msg Message) (Message, error)
	CallCount  int
}

func (m *MockHandler) Handle(ctx context.Context, peerID peer.ID, msg Message) (Message, error) {
	m.CallCount++
	if m.HandleFunc != nil {
		return m.HandleFunc(ctx, peerID, msg)
	}
	return nil, nil
}

func TestMockHandler_ImplementsHandler(t *testing.T) {
	mock := &MockHandler{
		HandleFunc: func(ctx context.Context, peerID peer.ID, msg Message) (Message, error) {
			return "mocked response", nil
		},
	}

	var handler Handler = mock

	resp, err := handler.Handle(context.Background(), peer.ID("test"), "msg")

	require.NoError(t, err)
	assert.Equal(t, "mocked response", resp)
	assert.Equal(t, 1, mock.CallCount)
}
