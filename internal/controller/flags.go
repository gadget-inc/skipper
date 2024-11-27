package controller

import (
	"os"
	"strconv"
	"strings"

	"github.com/gadget-inc/fusion/internal/flag"
)

var (
	FlagNamespace = flag.Flag[string]{
		Name:        "controller-namespace",
		Description: "The namespace the controller is in.",
		Required:    true,
	}

	FlagIP = flag.Flag[string]{
		Name:        "controller-ip",
		Description: "The IP the controller listens on.",
		Required:    true,
	}

	FlagPort = flag.Flag[int]{
		Name:        "controller-port",
		Description: "The port the controller listens on.",
		Default:     8080,
		Parse: func(s string) (int, error) {
			if strings.HasPrefix(s, "tcp://") && s == os.Getenv("FUSION_CONTROLLER_PORT") {
				// this environment variable was set by kubernetes, ignore it and use the default
				return 8080, nil
			}
			return strconv.Atoi(s)
		},
	}
)
