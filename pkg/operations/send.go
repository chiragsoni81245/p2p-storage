package operations

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chiragsoni81245/p2p-storage/internal/config"
	"github.com/chiragsoni81245/p2p-storage/internal/fileserver"
	"github.com/libp2p/go-libp2p/core/peer"
)

// SendFile stores a file locally then sends it directly to the peer at peerAddr.
// It blocks until the transfer completes or ctx is cancelled.
func SendFile(ctx context.Context, fs *fileserver.FileServer, cfg *config.YAMLConfig, filePath, peerAddr string, opts SendOpts) (*SendResult, error) {
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("file does not exist: %s", filePath)
	}
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("cannot send a directory: %s", filePath)
	}

	key, err := GetFileKey(filePath)
	if err != nil {
		return nil, err
	}

	// Store locally first if needed
	if !fs.HasFile(key) {
		storeCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
		_, storeErr := fs.StoreFile(storeCtx, key, filePath, 0)
		cancel()
		if storeErr != nil {
			return nil, fmt.Errorf("failed to store file locally: %w", storeErr)
		}
	}

	var targetPeerID peer.ID

	if !strings.Contains(peerAddr, "p2p-circuit") {
		// Direct address
		connCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
		targetPeerID, err = fs.ConnectToPeer(connCtx, peerAddr)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("failed to connect to peer: %w", err)
		}
	} else {
		// Relayed address: connect to relay, wait for reservation, then connect to target
		relayAddr := strings.Split(peerAddr, "/p2p-circuit/")[0]

		relayCtx, relayCancel := context.WithTimeout(ctx, cfg.Timeout)
		relayPeerID, relayErr := fs.ConnectToPeer(relayCtx, relayAddr)
		relayCancel()
		if relayErr != nil {
			return nil, fmt.Errorf("failed to connect to relay: %w", relayErr)
		}

		// Wait for a relay reservation to appear in our addresses
		reservationCtx, reservationCancel := context.WithTimeout(ctx, cfg.Timeout)
		defer reservationCancel()

		ticker := time.NewTicker(5 * time.Second)
	reservationLoop:
		for {
			select {
			case <-reservationCtx.Done():
				ticker.Stop()
				return nil, fmt.Errorf("timed out waiting for relay reservation after %s", cfg.Timeout)
			case <-ticker.C:
				for _, addr := range fs.GetNodeAddresses() {
					if strings.Contains(addr, relayPeerID.String()) {
						break reservationLoop
					}
				}
			}
		}
		ticker.Stop()

		// Connect to the target peer through the relay
		peerCtx, peerCancel := context.WithTimeout(ctx, cfg.Timeout)
		targetPeerID, err = fs.ConnectToPeer(peerCtx, peerAddr)
		peerCancel()
		if err != nil {
			return nil, fmt.Errorf("failed to connect to peer through relay: %w", err)
		}

		if !opts.AllowRelay {
			hpWait := opts.HolePunchWait
			if hpWait == 0 {
				hpWait = 10 * time.Second
			}
			hpCtx, hpCancel := context.WithTimeout(ctx, hpWait)
			hpErr := fs.WaitForDirectConnection(hpCtx, targetPeerID, hpWait)
			hpCancel()
			if hpErr != nil && !fs.IsDirectConnection(targetPeerID) {
				return nil, fmt.Errorf("cannot establish direct connection to peer (only relayed connection available): " +
					"use AllowRelay to permit relayed transfers")
			}
		}
	}

	// Transfer the file
	transferCtx, transferCancel := context.WithTimeout(ctx, cfg.Timeout)
	defer transferCancel()

	if opts.AllowRelay {
		err = fs.StoreFileToPeer(transferCtx, targetPeerID, key, opts.Session)
	} else {
		err = fs.StoreFileToPeerDirect(transferCtx, targetPeerID, key, opts.Session)
	}
	if err != nil {
		if err == fileserver.ErrRelayedConnection {
			return nil, fmt.Errorf("connection became relayed during transfer - aborting for security")
		}
		return nil, fmt.Errorf("failed to send file: %w", err)
	}

	return &SendResult{
		Key:    key,
		PeerID: targetPeerID.String(),
	}, nil
}
