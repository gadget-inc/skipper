package fixture

import (
	"os"

	"github.com/gadget-inc/fusion/internal/log"
)

func init() {
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "error"
	}

	err := log.FlagLogLevel.Set(logLevel)
	if err != nil {
		panic(err)
	}

	logFormat := os.Getenv("LOG_FORMAT")
	if logFormat == "" {
		logFormat = "text"
	}

	err = log.FlagLogFormat.Set(logFormat)
	if err != nil {
		panic(err)
	}

	log.Init()
}
