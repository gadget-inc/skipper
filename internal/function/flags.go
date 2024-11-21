package function

import "github.com/gadget-inc/fusion/internal/flag"

var (
	FlagNamespaces = flag.Flag[[]string]{
		Name:        "function-namespaces",
		Description: "The namespaces where functions can be invoked.",
		Required:    true,
	}

	FlagPort = flag.Flag[int]{
		Name:        "function-port",
		Description: "The port on which the function server listens.",
		Default:     8888,
	}
)
