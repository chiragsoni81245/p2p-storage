package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/core"
	"github.com/chiragsoni81245/p2p-storage/internal/middleware"
	"github.com/chiragsoni81245/p2p-storage/internal/observability"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2pprotocol "github.com/libp2p/go-libp2p/core/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestHosts(t *testing.T) (host.Host, host.Host, func()) {
	h1, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)

	h2, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)

	// Connect h1 to h2
	h1.Peerstore().AddAddrs(h2.ID(), h2.Addrs(), time.Hour)
	h2.Peerstore().AddAddrs(h1.ID(), h1.Addrs(), time.Hour)

	err = h1.Connect(context.Background(), peer.AddrInfo{ID: h2.ID(), Addrs: h2.Addrs()})
	require.NoError(t, err)

	return h1, h2, func() {
		h1.Close()
		h2.Close()
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, int64(1<<20), cfg.MaxMessageSize)
	assert.Equal(t, 5*time.Second, cfg.ReadTimeout)
	assert.Equal(t, 5*time.Second, cfg.WriteTimeout)
	assert.Equal(t, 5*time.Second, cfg.HandlerTimeout)
}

func TestConfig_CustomValues(t *testing.T) {
	cfg := Config{
		MaxMessageSize: 1024,
		ReadTimeout:    time.Second,
		WriteTimeout:   2 * time.Second,
		HandlerTimeout: 3 * time.Second,
	}

	assert.Equal(t, int64(1024), cfg.MaxMessageSize)
	assert.Equal(t, time.Second, cfg.ReadTimeout)
	assert.Equal(t, 2*time.Second, cfg.WriteTimeout)
	assert.Equal(t, 3*time.Second, cfg.HandlerTimeout)
}

func TestMessage_Struct(t *testing.T) {
	msg := Message{
		Type: "PING",
		Key:  "test-key",
		Data: "test-data",
	}

	assert.Equal(t, "PING", msg.Type)
	assert.Equal(t, "test-key", msg.Key)
	assert.Equal(t, "test-data", msg.Data)
}

func TestMessage_JSONSerialization(t *testing.T) {
	original := Message{
		Type: "TEST",
		Key:  "key",
		Data: "data",
	}

	// Encode to JSON
	data, err := json.Marshal(original)
	require.NoError(t, err)

	// Decode back
	var decoded Message
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original, decoded)
}

func TestPingHandler_Handle(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := observability.NewLoggerWithWriter(buf, observability.Fields{})

	handler := &PingHandler{Logger: logger}

	msg := Message{Type: "PING", Data: "hello"}

	resp, err := handler.Handle(context.Background(), peer.ID("test-peer"), msg)
	require.NoError(t, err)

	respMsg, ok := resp.(Message)
	require.True(t, ok)
	assert.Equal(t, "PONG", respMsg.Type)
	assert.Equal(t, "hello back", respMsg.Data)
}

func TestPingHandler_Handle_InvalidMessageType(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := observability.NewLoggerWithWriter(buf, observability.Fields{})

	handler := &PingHandler{Logger: logger}

	// Pass wrong type
	resp, err := handler.Handle(context.Background(), peer.ID("test"), "not a Message")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid message type")
	assert.Nil(t, resp)
}

func TestPingHandler_ImplementsHandler(t *testing.T) {
	var _ core.Handler = &PingHandler{}
}

func TestNew_RegistersStreamHandler(t *testing.T) {
	h1, _, cleanup := createTestHosts(t)
	defer cleanup()

	buf := &bytes.Buffer{}
	logger := observability.NewLoggerWithWriter(buf, observability.Fields{})
	limiter := middleware.NewLimiter(10)
	handler := &PingHandler{Logger: logger}
	cfg := DefaultConfig()

	protocolID := libp2pprotocol.ID("/test/1.0.0")
	_ = New(h1, protocolID, handler, cfg, logger, limiter)

	// Verify stream handler is registered by checking protocols
	protocols := h1.Mux().Protocols()
	found := false
	for _, p := range protocols {
		if p == protocolID {
			found = true
			break
		}
	}
	assert.True(t, found, "protocol should be registered")
}

func TestProtocol_Send_Success(t *testing.T) {
	h1, h2, cleanup := createTestHosts(t)
	defer cleanup()

	buf := &bytes.Buffer{}
	logger := observability.NewLoggerWithWriter(buf, observability.Fields{})
	limiter := middleware.NewLimiter(10)
	handler := &PingHandler{Logger: logger}
	cfg := DefaultConfig()

	protocolID := libp2pprotocol.ID("/test/ping/1.0.0")

	// Register protocol on h2 (receiver)
	_ = New(h2, protocolID, handler, cfg, logger, limiter)

	// Create protocol on h1 (sender)
	proto := New(h1, protocolID, handler, cfg, logger, limiter)

	// Send message from h1 to h2
	msg := Message{Type: "PING", Data: "hello"}
	resp, err := proto.Send(context.Background(), h2.ID(), msg)

	require.NoError(t, err)
	assert.Equal(t, "PONG", resp.Type)
	assert.Equal(t, "hello back", resp.Data)
}

func TestProtocol_Send_MultipleMessages(t *testing.T) {
	h1, h2, cleanup := createTestHosts(t)
	defer cleanup()

	buf := &bytes.Buffer{}
	logger := observability.NewLoggerWithWriter(buf, observability.Fields{})
	limiter := middleware.NewLimiter(10)
	handler := &PingHandler{Logger: logger}
	cfg := DefaultConfig()

	protocolID := libp2pprotocol.ID("/test/multi/1.0.0")

	_ = New(h2, protocolID, handler, cfg, logger, limiter)
	proto := New(h1, protocolID, handler, cfg, logger, limiter)

	// Send multiple messages
	for i := 0; i < 5; i++ {
		msg := Message{Type: "PING", Data: "hello"}
		resp, err := proto.Send(context.Background(), h2.ID(), msg)

		require.NoError(t, err)
		assert.Equal(t, "PONG", resp.Type)
	}
}

func TestProtocol_Send_UnknownPeer(t *testing.T) {
	h1, _, cleanup := createTestHosts(t)
	defer cleanup()

	buf := &bytes.Buffer{}
	logger := observability.NewLoggerWithWriter(buf, observability.Fields{})
	limiter := middleware.NewLimiter(10)
	handler := &PingHandler{Logger: logger}
	cfg := Config{
		MaxMessageSize: 1024,
		ReadTimeout:    500 * time.Millisecond,
		WriteTimeout:   500 * time.Millisecond,
		HandlerTimeout: 500 * time.Millisecond,
	}

	protocolID := libp2pprotocol.ID("/test/unknown/1.0.0")
	proto := New(h1, protocolID, handler, cfg, logger, limiter)

	// Try to send to unknown peer
	unknownPeer, _ := peer.Decode("QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N")
	_, err := proto.Send(context.Background(), unknownPeer, Message{Type: "PING"})

	assert.Error(t, err)
}

func TestProtocol_Send_ContextCancellation(t *testing.T) {
	h1, h2, cleanup := createTestHosts(t)
	defer cleanup()

	buf := &bytes.Buffer{}
	logger := observability.NewLoggerWithWriter(buf, observability.Fields{})
	limiter := middleware.NewLimiter(10)

	// Slow handler
	slowHandler := core.HandlerFunc(func(ctx context.Context, peerID peer.ID, msg core.Message) (core.Message, error) {
		time.Sleep(5 * time.Second)
		return Message{Type: "PONG"}, nil
	})

	cfg := DefaultConfig()
	protocolID := libp2pprotocol.ID("/test/cancel/1.0.0")

	_ = New(h2, protocolID, slowHandler, cfg, logger, limiter)
	proto := New(h1, protocolID, slowHandler, cfg, logger, limiter)

	// Create cancelled context
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := proto.Send(ctx, h2.ID(), Message{Type: "PING"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "i/o deadline reached")
}

func TestProtocol_HandleStream_BackpressureRejection(t *testing.T) {
	h1, h2, cleanup := createTestHosts(t)
	defer cleanup()

	buf := &bytes.Buffer{}
	logger := observability.NewLoggerWithWriter(buf, observability.Fields{})
	limiter := middleware.NewLimiter(1) // Only allow 1 concurrent request

	// Slow handler to hold the slot
	slowHandler := core.HandlerFunc(func(ctx context.Context, peerID peer.ID, msg core.Message) (core.Message, error) {
		time.Sleep(500 * time.Millisecond)
		return Message{Type: "PONG"}, nil
	})

	cfg := DefaultConfig()
	protocolID := libp2pprotocol.ID("/test/backpressure/1.0.0")

	_ = New(h2, protocolID, slowHandler, cfg, logger, limiter)
	proto := New(h1, protocolID, slowHandler, cfg, logger, limiter)

	// Start first request (will hold the slot)
	go func() {
		proto.Send(context.Background(), h2.ID(), Message{Type: "PING"})
	}()

	// Give it time to acquire slot
	time.Sleep(50 * time.Millisecond)

	// Second request should be rejected due to backpressure
	// This will fail because the receiver will reset the stream
	_, err := proto.Send(context.Background(), h2.ID(), Message{Type: "PING"})

	// The error indicates the stream was reset (backpressure)
	// This is expected behavior
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stream reset")
}

func TestProtocol_HandleStream_HandlerError(t *testing.T) {
	h1, h2, cleanup := createTestHosts(t)
	defer cleanup()

	buf := &bytes.Buffer{}
	logger := observability.NewLoggerWithWriter(buf, observability.Fields{})
	limiter := middleware.NewLimiter(10)

	errorHandler := core.HandlerFunc(func(ctx context.Context, peerID peer.ID, msg core.Message) (core.Message, error) {
		return nil, errors.New("handler error")
	})

	cfg := DefaultConfig()
	protocolID := libp2pprotocol.ID("/test/error/1.0.0")

	_ = New(h2, protocolID, errorHandler, cfg, logger, limiter)
	proto := New(h1, protocolID, errorHandler, cfg, logger, limiter)

	_, err := proto.Send(context.Background(), h2.ID(), Message{Type: "PING"})

	// The sender will get an error because the handler didn't write a response
	// (stream closes without proper response, resulting in EOF or similar)
	assert.Error(t, err)
}

func TestProtocol_Bidirectional(t *testing.T) {
	h1, h2, cleanup := createTestHosts(t)
	defer cleanup()

	buf := &bytes.Buffer{}
	logger := observability.NewLoggerWithWriter(buf, observability.Fields{})
	limiter := middleware.NewLimiter(10)
	handler := &PingHandler{Logger: logger}
	cfg := DefaultConfig()

	protocolID := libp2pprotocol.ID("/test/bidirectional/1.0.0")

	// Both hosts can send and receive
	proto1 := New(h1, protocolID, handler, cfg, logger, limiter)
	proto2 := New(h2, protocolID, handler, cfg, logger, limiter)

	// h1 sends to h2
	resp1, err := proto1.Send(context.Background(), h2.ID(), Message{Type: "PING"})
	require.NoError(t, err)
	assert.Equal(t, "PONG", resp1.Type)

	// h2 sends to h1
	resp2, err := proto2.Send(context.Background(), h1.ID(), Message{Type: "PING"})
	require.NoError(t, err)
	assert.Equal(t, "PONG", resp2.Type)
}

func TestProtocol_CustomHandler(t *testing.T) {
	h1, h2, cleanup := createTestHosts(t)
	defer cleanup()

	buf := &bytes.Buffer{}
	logger := observability.NewLoggerWithWriter(buf, observability.Fields{})
	limiter := middleware.NewLimiter(10)

	// Custom echo handler
	echoHandler := core.HandlerFunc(func(ctx context.Context, peerID peer.ID, msg core.Message) (core.Message, error) {
		m := msg.(Message)
		return Message{
			Type: "ECHO",
			Key:  m.Key,
			Data: m.Data,
		}, nil
	})

	cfg := DefaultConfig()
	protocolID := libp2pprotocol.ID("/test/echo/1.0.0")

	_ = New(h2, protocolID, echoHandler, cfg, logger, limiter)
	proto := New(h1, protocolID, echoHandler, cfg, logger, limiter)

	resp, err := proto.Send(context.Background(), h2.ID(), Message{
		Type: "TEST",
		Key:  "mykey",
		Data: "mydata",
	})

	require.NoError(t, err)
	assert.Equal(t, "ECHO", resp.Type)
	assert.Equal(t, "mykey", resp.Key)
	assert.Equal(t, "mydata", resp.Data)
}
