package router

import (
	"time"

	"github.com/gadget-inc/skipper/internal/config"
	"github.com/gadget-inc/skipper/internal/fixture"
)

func testConfig() *Config {
	cfg := config.New[Config]()
	cfg.HeartbeatInterval = 100 * time.Millisecond
	cfg.PodIP = fixture.RouterIP
	cfg.RoundTripRetryMaxTimeout = 10 * time.Millisecond
	return cfg
}
