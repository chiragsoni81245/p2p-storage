package node

import (
	"fmt"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
)

func NewNode(cfg Config) (host.Host, error) {
	node, err := libp2p.New(
		libp2p.ListenAddrStrings(
			fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", cfg.ListenPort),
		),

		// Default stack
		libp2p.DefaultTransports,
		libp2p.DefaultSecurity,
		libp2p.DefaultMuxers,

		// Helps with NAT
		libp2p.NATPortMap(),
	)
	if err != nil {
		return nil, err
	}

	return node, nil
}
