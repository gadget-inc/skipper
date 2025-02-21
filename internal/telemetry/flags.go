package telemetry

import "github.com/gadget-inc/fusion/internal/flag"

var FlagTelemetry = flag.Flag[bool]{
	Name:        "telemetry",
	Default:     false,
	Description: "Whether to enable OpenTelemetry",
}
