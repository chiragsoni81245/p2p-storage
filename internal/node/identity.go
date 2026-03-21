package node

import (
	"os"

	crypto "github.com/libp2p/go-libp2p/core/crypto"
)

func LoadOrCreateIdentity(path string) (crypto.PrivKey, error) {
	// Check if key exists
	if _, err := os.Stat(path); err == nil {
		// Load existing key
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}

		priv, err := crypto.UnmarshalPrivateKey(data)
		if err != nil {
			return nil, err
		}

		return priv, nil
	}

	// Generate new key
	priv, _, err := crypto.GenerateKeyPair(crypto.Ed25519, -1)
	if err != nil {
		return nil, err
	}

	// Serialize
	data, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		return nil, err
	}

	// Save to disk
	if err := os.WriteFile(path, data, 0600); err != nil {
		return nil, err
	}

	return priv, nil
}
