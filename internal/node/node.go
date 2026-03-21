package node

import (
	"fmt"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	connmgr "github.com/libp2p/go-libp2p/p2p/net/connmgr"
)

func NewNode(cfg Config) (host.Host, error) {
	cm, err := connmgr.NewConnManager(
		cfg.MinConnection,  // low watermark
		cfg.MaxConnection,  // high watermark
	)

	if err != nil {
		return nil, err
	}

	priv, err := LoadOrCreateIdentity(cfg.IdentityPath)
	if err != nil {
		return nil, err
	}

	node, err := libp2p.New(
		libp2p.Identity(priv),

		libp2p.ListenAddrStrings(
			fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", cfg.ListenPort),
		),

		// Default stack
		libp2p.DefaultTransports,
		libp2p.DefaultSecurity,
		libp2p.DefaultMuxers,

		// Conn Manager
		libp2p.ConnectionManager(cm),

		// Helps with NAT
		libp2p.NATPortMap(),
	)
	if err != nil {
		return nil, err
	}

	return node, nil
}
