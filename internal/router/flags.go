package router

import (
	"os"
	"strconv"
	"strings"

	"github.com/gadget-inc/fusion/internal/flag"
)

var (
	FlagPort = flag.Flag[int]{
		Name:        "router-port",
		Description: "The port the router listens on.",
		Default:     8080,
		Parse: func(s string) (int, error) {
			if strings.HasPrefix(s, "tcp://") && s == os.Getenv("FUSION_ROUTER_PORT") {
				// this environment variable was set by kubernetes, ignore it and use the default
				return 8080, nil
			}
			return strconv.Atoi(s)
		},
	}

	FlagControllerServiceHost = flag.Flag[string]{
		Name:        "controller-service-host",
		Description: "The hostname of the controller service.",
		Required:    true,
	}

	FlagControllerServicePort = flag.Flag[int]{
		Name:        "controller-service-port",
		Description: "The port the controller service listens on.",
		Default:     80,
	}
)
