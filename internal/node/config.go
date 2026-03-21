package node

type Config struct {
	ListenPort int
	IdentityPath string
	MinConnection int
	MaxConnection int
}

func DefaultConfig() Config {
	return Config{
		ListenPort: 0,
		IdentityPath: "./node.key",
		MinConnection: 50,
		MaxConnection: 100,
	}
}
