package fixture

import (
	"os"

	"github.com/gadget-inc/skipper/internal/config"
	"github.com/gadget-inc/skipper/internal/log"
)

func init() {
	cfg := config.New[log.Config]()

	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "error"
	}
	if err := cfg.Level.UnmarshalText([]byte(logLevel)); err != nil {
		panic(err)
	}

	logFormat := os.Getenv("LOG_FORMAT")
	if logFormat != "" {
		cfg.Format = logFormat
	} else {
		cfg.Format = "text"
	}

	_ = log.Init(cfg)
}
