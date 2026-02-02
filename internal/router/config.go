package router

import (
	"time"
)

// Config holds the router configuration.
type Config struct {
	Host                      string        `flag:"host" description:"The hostname the router listens on." default:"0.0.0.0"`
	Port                      int           `flag:"port" description:"The port the router listens on." default:"8080"`
	ShutdownTimeout           time.Duration `flag:"shutdown-timeout" description:"The timeout for shutting down the router." default:"5s"`
	PodIP                     string        `flag:"pod-ip" description:"The pod IP the router is running on." required:"true"`
	HeartbeatInterval         time.Duration `flag:"heartbeat-interval" description:"The interval at which to send heartbeats to the controller." default:"5s"`
	MaxRoundTripAttempts      int           `flag:"max-round-trip-attempts" description:"The maximum number of attempts to proxy a request to a function." default:"6"`
	RoundTripRetryMinTimeout  time.Duration `flag:"round-trip-retry-min-timeout" description:"The minimum timeout between round trip attempts." default:"100ms"`
	RoundTripRetryMaxTimeout  time.Duration `flag:"round-trip-retry-max-timeout" description:"The maximum timeout between round trip attempts." default:"5s"`
	ControllerServiceHost     string        `flag:"controller-service-host" description:"The hostname of the controller service." required:"true"`
	ControllerHTTPPort        int           `flag:"controller-http-port" description:"The HTTP port the controller service listens on." default:"80"`
	ControllerProtocol        string        `flag:"controller-protocol" description:"Protocol to use for controller communication (http or grpc)." default:"http"`
	ControllerGRPCPort        int           `flag:"controller-grpc-port" description:"The gRPC port the controller service listens on." default:"50051"`
	ControllerGRPCServiceHost string        `flag:"controller-grpc-service-host" description:"The hostname of the headless controller service for gRPC. Falls back to controller-service-host if not set."`
}
