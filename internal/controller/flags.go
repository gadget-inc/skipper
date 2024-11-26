package controller

import "github.com/gadget-inc/fusion/internal/flag"

var (
	FlagNamespace = flag.Flag[string]{
		Name:        "controller-namespace",
		Description: "The namespace this controller instance is in.",
		Required:    true,
	}

	FlagIP = flag.Flag[string]{
		Name:        "controller-ip",
		Description: "The IP address assigned to this controller.",
		Required:    true,
	}

	FlagPort = flag.Flag[int]{
		Name:        "controller-port",
		Description: "The port this controller is listening on.",
		Default:     8080,
	}

	FlagServiceHost = flag.Flag[string]{
		Name:        "controller-service-host",
		Description: "The hostname of the controller service.",
		Required:    true,
	}

	FlagServicePort = flag.Flag[int]{
		Name:        "controller-service-port",
		Description: "The port of the controller service.",
		Default:     80,
	}
)
