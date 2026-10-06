package toolrouter

type Config struct {
    ComputePollSec int
}

func DefaultConfig() Config {
    return Config{ComputePollSec: 30}
}
