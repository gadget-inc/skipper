package controller

import "github.com/gadget-inc/fusion/internal/flag"

var (
	FlagIP = flag.Flag[string]{
		Name:        "controller-ip",
		Description: "The IP address of this controller instance.",
		Required:    true,
	}

	FlagNamespace = flag.Flag[string]{
		Name:        "controller-namespace",
		Description: "The namespace this controller instance is in.",
		Required:    true,
	}

	FlagHost = flag.Flag[string]{
		Name:        "controller-host",
		Description: "The hostname of the controller service.",
		Required:    true,
	}

	FlagPort = flag.Flag[int]{
		Name:        "controller-port",
		Description: "The port of the controller service.",
		Default:     8080,
	}
)
