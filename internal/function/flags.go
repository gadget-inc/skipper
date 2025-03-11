package function

import (
	"time"

	"github.com/gadget-inc/fusion/internal/flag"
)

var (
	FlagNamespaces = flag.Flag[[]string]{
		Name:        "function-namespaces",
		Description: "The namespaces where functions can be invoked.",
		Required:    true,
	}

	FlagAssignPath = flag.Flag[string]{
		Name:        "function-assign-path",
		Description: "The path used to assign a function to a pod.",
		Default:     "/__fusion/assign",
	}

	FlagAssignTimeout = flag.Flag[time.Duration]{
		Name:        "function-assign-timeout",
		Description: "The timeout for assigning a function to a pod.",
		Default:     30 * time.Second,
	}

	FlagSkipForbiddenNamespaces = flag.Flag[bool]{
		Name:        "skip-forbidden-namespaces",
		Description: "Whether to skip function namespaces that the service account does not have access to.",
		Default:     false,
	}
)
