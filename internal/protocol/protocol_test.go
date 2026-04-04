//go:build integration

package protocol

import (
	"bytes"
	"context"
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

func TestPingHandler_Handle(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := observability.NewLoggerWithWriter(buf, observability.Fields{})

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

func TestPingHandler_Handle_InvalidMessageType(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := observability.NewLoggerWithWriter(buf, observability.Fields{})

	handler := &PingHandler{Logger: logger}

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

	_ = New(h2, protocolID, handler, cfg, logger, limiter)
	proto := New(h1, protocolID, handler, cfg, logger, limiter)

	msg := NewMessage("PING", "hello")
	resp, err := proto.Send(context.Background(), h2.ID(), msg)

	require.NoError(t, err)
	assert.Equal(t, "PONG", resp.Type)

	data, err := Decode[string](resp)
	require.NoError(t, err)
	assert.Equal(t, "hello back", data)
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

	for i := 0; i < 5; i++ {
		resp, err := proto.Send(context.Background(), h2.ID(), NewMessage("PING", "hello"))
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

	unknownPeer, _ := peer.Decode("QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N")
	_, err := proto.Send(context.Background(), unknownPeer, NewMessage("PING", nil))

	assert.Error(t, err)
}

func TestProtocol_Send_ContextCancellation(t *testing.T) {
	h1, h2, cleanup := createTestHosts(t)
	defer cleanup()

	buf := &bytes.Buffer{}
	logger := observability.NewLoggerWithWriter(buf, observability.Fields{})
	limiter := middleware.NewLimiter(10)

	slowHandler := core.HandlerFunc(func(ctx context.Context, peerID peer.ID, msg core.Message) (core.Message, error) {
		time.Sleep(5 * time.Second)
		return NewMessage("PONG", nil), nil
	})

	cfg := DefaultConfig()
	protocolID := libp2pprotocol.ID("/test/cancel/1.0.0")

	_ = New(h2, protocolID, slowHandler, cfg, logger, limiter)
	proto := New(h1, protocolID, slowHandler, cfg, logger, limiter)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := proto.Send(ctx, h2.ID(), NewMessage("PING", nil))
	assert.Error(t, err)
}

func TestProtocol_HandleStream_BackpressureRejection(t *testing.T) {
	h1, h2, cleanup := createTestHosts(t)
	defer cleanup()

	buf := &bytes.Buffer{}
	logger := observability.NewLoggerWithWriter(buf, observability.Fields{})
	limiter := middleware.NewLimiter(1)

	slowHandler := core.HandlerFunc(func(ctx context.Context, peerID peer.ID, msg core.Message) (core.Message, error) {
		time.Sleep(500 * time.Millisecond)
		return NewMessage("PONG", nil), nil
	})

	cfg := DefaultConfig()
	protocolID := libp2pprotocol.ID("/test/backpressure/1.0.0")

	_ = New(h2, protocolID, slowHandler, cfg, logger, limiter)
	proto := New(h1, protocolID, slowHandler, cfg, logger, limiter)

	go func() {
		proto.Send(context.Background(), h2.ID(), NewMessage("PING", nil))
	}()

	time.Sleep(50 * time.Millisecond)

	_, err := proto.Send(context.Background(), h2.ID(), NewMessage("PING", nil))
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

	_, err := proto.Send(context.Background(), h2.ID(), NewMessage("PING", nil))
	assert.Error(t, err)
}

func TestProtocol_SendOneWay_Success(t *testing.T) {
	h1, h2, cleanup := createTestHosts(t)
	defer cleanup()

	buf := &bytes.Buffer{}
	logger := observability.NewLoggerWithWriter(buf, observability.Fields{})
	limiter := middleware.NewLimiter(10)

	received := make(chan Message, 1)
	handler := core.HandlerFunc(func(ctx context.Context, peerID peer.ID, msg core.Message) (core.Message, error) {
		received <- msg.(Message)
		return nil, nil // fire-and-forget: no response
	})

	cfg := DefaultConfig()
	protocolID := libp2pprotocol.ID("/test/oneway/1.0.0")

	_ = New(h2, protocolID, handler, cfg, logger, limiter)
	proto := New(h1, protocolID, handler, cfg, logger, limiter)

	err := proto.SendOneWay(context.Background(), h2.ID(), NewMessage("NOTIFY", "payload"))
	require.NoError(t, err)

	select {
	case msg := <-received:
		assert.Equal(t, "NOTIFY", msg.Type)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for one-way message")
	}
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

	proto1 := New(h1, protocolID, handler, cfg, logger, limiter)
	proto2 := New(h2, protocolID, handler, cfg, logger, limiter)

	resp1, err := proto1.Send(context.Background(), h2.ID(), NewMessage("PING", nil))
	require.NoError(t, err)
	assert.Equal(t, "PONG", resp1.Type)

	resp2, err := proto2.Send(context.Background(), h1.ID(), NewMessage("PING", nil))
	require.NoError(t, err)
	assert.Equal(t, "PONG", resp2.Type)
}
