package router

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gadget-inc/skipper/internal/flag"
)

var (
	FlagHost = flag.Flag[string]{
		Name:        "router-host",
		Description: "The hostname the router listens on.",
		Default:     "0.0.0.0",
	}

	FlagPort = flag.Flag[int]{
		Name:        "router-port",
		Description: "The port the router listens on.",
		Default:     8080,
		Parse: func(s string) (int, error) {
			if strings.HasPrefix(s, "tcp://") && s == os.Getenv("SKIPPER_ROUTER_PORT") {
				// this environment variable was set by kubernetes, ignore it and use the default
				return 8080, nil
			}
			return strconv.Atoi(s)
		},
	}

	FlagShutdownTimeout = flag.Flag[time.Duration]{
		Name:        "router-shutdown-timeout",
		Description: "The timeout for shutting down the router.",
		Default:     5 * time.Second,
	}

	FlagPodIP = flag.Flag[string]{
		Name:        "router-pod-ip",
		Description: "The pod IP the router is running on.",
		Required:    true,
	}

	FlagHeartbeatInterval = flag.Flag[time.Duration]{
		Name:        "router-heartbeat-interval",
		Description: "The interval at which to send heartbeats to the controller.",
		Default:     5 * time.Second,
	}

	FlagMaxRoundTripAttempts = flag.Flag[int]{
		Name:        "router-max-round-trip-attempts",
		Description: "The maximum number of attempts to proxy a request to a function.",
		Default:     6,
	}

	FlagRoundTripRetryMinTimeout = flag.Flag[time.Duration]{
		Name:        "router-round-trip-retry-min-timeout",
		Description: "The minimum timeout between round trip attempts.",
		Default:     100 * time.Millisecond,
	}

	FlagRoundTripRetryMaxTimeout = flag.Flag[time.Duration]{
		Name:        "router-round-trip-retry-max-timeout",
		Description: "The maximum timeout between round trip attempts.",
		Default:     5 * time.Second,
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

	FlagControllerServiceGRPCPort = flag.Flag[int]{
		Name:        "controller-service-grpc-port",
		Description: "The port the controller service listens on for gRPC.",
		Default:     5051,
	}
)
