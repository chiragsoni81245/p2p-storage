package node

type Config struct {
	ListenPort int
	MinConnection int
	MaxConnection int
}

func DefaultConfig() Config {
	return Config{
		ListenPort: 0,
		MinConnection: 50,
		MaxConnection: 100,
	}
}
