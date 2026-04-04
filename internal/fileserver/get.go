package fileserver

import (
	"context"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/event"
	"github.com/chiragsoni81245/p2p-storage/internal/observability"
	"github.com/chiragsoni81245/p2p-storage/internal/protocol"
	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	DefaultTTL        = 3
	GetConnectTimeout = 30 * time.Second
)

// processGet processes an incoming GET_FILE payload.
// Always called in a goroutine — no response is written back.
func (fs *FileServer) processGet(senderID peer.ID, payload protocol.GetFilePayload) {
	if !fs.seen.See(payload.MsgID) {
		return
	}

	if fs.store.Has(payload.Key) {
		fs.bus.Publish(event.Event{
			Type: event.GetDelivering,
			Data: event.GetDeliveringData{
				MsgID:       payload.MsgID,
				Key:         payload.Key,
				RequesterID: payload.RequesterID,
			},
		})
		go fs.deliverToRequester(payload)
		return
	}

	fs.bus.Publish(event.Event{
		Type: event.GetReceived,
		Data: event.GetReceivedData{
			MsgID: payload.MsgID,
			Key:   payload.Key,
			TTL:   payload.TTL,
		},
	})

	if payload.TTL <= 0 {
		return
	}

	payload.TTL--
	peers := fs.selector.Select(fs.ListPeers(), senderID, 10)
	msg := protocol.NewMessage(protocol.TypeGetFile, payload)
	for _, p := range peers {
		go func() {
			fwdCtx, cancel := context.WithTimeout(context.Background(), GetConnectTimeout)
			defer cancel()
			if err := fs.proto.SendOneWay(fwdCtx, p, msg); err != nil {
				fs.logger.Debug("failed to forward GET_FILE", observability.Fields{
					"peer":  p.String(),
					"key":   payload.Key,
					"error": err.Error(),
				})
			}
		}()
	}
}

func (fs *FileServer) deliverToRequester(payload protocol.GetFilePayload) {
	requesterID, err := peer.Decode(payload.RequesterID)
	if err != nil {
		fs.logger.Error("failed to decode requester peer ID", observability.Fields{
			"requester_id": payload.RequesterID,
			"error":        err.Error(),
		})
		return
	}

	// Try direct addresses first, then relay circuit addresses.
	// ConnectWithHolePunch establishes the best available connection (direct after
	// hole punching, or relay as fallback). Whether relay is acceptable for the
	// transfer itself is checked separately below.
	opts := ConnectOpts{
		HolePunchWait: fs.Config.NodeConfig.HolePunchWait,
	}

	connected := false
	for _, addr := range payload.RequesterAddrs {
		if _, connErr := fs.ConnectWithHolePunch(context.Background(), addr, opts); connErr == nil {
			connected = true
			break
		} else {
			fs.logger.Debug("failed to connect to requester", observability.Fields{
				"addr":  addr,
				"error": connErr.Error(),
			})
		}
	}

	if !connected {
		fs.logger.Error("failed to connect to requester for file delivery", observability.Fields{
			"requester_id": payload.RequesterID,
			"key":          payload.Key,
		})
		return
	}

	// Transfer: no deadline — let the stream run to completion.
	// Always require a direct connection; relay transfers are not allowed.
	err = fs.StoreFileToPeerDirect(context.Background(), requesterID, payload.Key, "")
	if err != nil && err != ErrFileAlreadyExists {
		fs.logger.Error("failed to deliver file to requester", observability.Fields{
			"requester_id": payload.RequesterID,
			"key":          payload.Key,
			"error":        err.Error(),
		})
		return
	}

	fs.bus.Publish(event.Event{
		Type: event.GetDelivered,
		Data: event.GetDeliveredData{
			MsgID: payload.MsgID,
			Key:   payload.Key,
		},
	})

	fs.logger.Info("file delivered to requester", observability.Fields{
		"requester_id": payload.RequesterID,
		"key":          payload.Key,
	})
}
