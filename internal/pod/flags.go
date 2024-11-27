package pod

import "github.com/gadget-inc/fusion/internal/flag"

var FlagSkipForbiddenNamespaces = flag.Flag[bool]{
	Name:        "skip-forbidden-namespaces",
	Description: "Skip namespaces that the controller does not have access to.",
	Default:     false,
}
