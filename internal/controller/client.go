package controller

import (
	"context"
	"fmt"

	"github.com/gadget-inc/skipper/internal/skipper"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Client is the interface for communicating with a controller.
type Client interface {
	Instance(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (instance *skipper.Instance, err error)
	Heartbeat(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error
	Scale(ctx context.Context, fn *skipper.Function, desiredInstances uint32, reason skipper.ScaleReason) ([]*skipper.Instance, error)
	ReleaseInstance(ctx context.Context, inst *skipper.Instance) error
	Close() error
}

// NewClientFunc creates a Client for the given host.
type NewClientFunc func(host string) Client

// client implements the Client interface.
type client struct {
	conn   *grpc.ClientConn
	client skipper.ControllerServiceClient
}

var _ Client = &client{}

// defaultServiceConfig configures gRPC client behavior including load balancing
// and retry policies. This is used with DNS-based service discovery where
// multiple A records are returned for a headless Kubernetes service.
//
// Retry policies are configured per-RPC:
//   - GetInstance: Retries on UNAVAILABLE since routing is critical
//   - Heartbeat: Retries on UNAVAILABLE to prevent premature pod termination
//   - Scale: Retries on UNAVAILABLE for reliability
const defaultServiceConfig = `{
	"loadBalancingConfig": [{"round_robin": {}}],
	"methodConfig": [
		{
			"name": [{"service": "skipper.ControllerService", "method": "GetInstance"}],
			"retryPolicy": {
				"maxAttempts": 3,
				"initialBackoff": "0.01s",
				"maxBackoff": "0.1s",
				"backoffMultiplier": 2,
				"retryableStatusCodes": ["UNAVAILABLE"]
			}
		},
		{
			"name": [{"service": "skipper.ControllerService", "method": "Heartbeat"}],
			"retryPolicy": {
				"maxAttempts": 2,
				"initialBackoff": "0.01s",
				"maxBackoff": "0.05s",
				"backoffMultiplier": 2,
				"retryableStatusCodes": ["UNAVAILABLE"]
			}
		},
		{
			"name": [{"service": "skipper.ControllerService", "method": "Scale"}],
			"retryPolicy": {
				"maxAttempts": 3,
				"initialBackoff": "0.05s",
				"maxBackoff": "0.5s",
				"backoffMultiplier": 2,
				"retryableStatusCodes": ["UNAVAILABLE"]
			}
		},
		{
			"name": [{"service": "skipper.ControllerService", "method": "ReleaseInstance"}],
			"retryPolicy": {
				"maxAttempts": 2,
				"initialBackoff": "0.01s",
				"maxBackoff": "0.05s",
				"backoffMultiplier": 2,
				"retryableStatusCodes": ["UNAVAILABLE"]
			}
		}
	]
}`

// NewClient creates a new client for the controller.
// When used with a headless Kubernetes service, the DNS resolver will
// return all pod IPs, and round-robin load balancing will distribute
// requests across all available controller pods.
func NewClient(host string, port int) (Client, error) {
	// Use dns:/// scheme to enable DNS-based service discovery.
	// This allows gRPC to resolve the service name to multiple IP addresses
	// (from a headless service) and load balance across them.
	target := fmt.Sprintf("dns:///%s:%d", host, port)
	conn, err := grpc.NewClient(target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithDefaultServiceConfig(defaultServiceConfig),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection: %w", err)
	}

	return &client{
		conn:   conn,
		client: skipper.NewControllerServiceClient(conn),
	}, nil
}

func (c *client) Instance(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
	req := &skipper.GetInstanceRequest{}
	req.SetFunction(fn)
	req.SetExcludeInstanceNames(excludeInstanceNames)

	resp, err := c.client.GetInstance(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}

	return resp.GetInstance(), nil
}

func (c *client) Heartbeat(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
	if len(heartbeats) == 0 {
		return nil
	}

	req := &skipper.HeartbeatRequest{}
	req.SetRouterIp(routerIP)
	req.SetHeartbeats(heartbeats)
	req.SetForwardedFor(forwardedFor)

	_, err := c.client.Heartbeat(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to send heartbeat: %w", err)
	}

	return nil
}

func (c *client) Scale(ctx context.Context, fn *skipper.Function, desiredInstances uint32, reason skipper.ScaleReason) ([]*skipper.Instance, error) {
	req := &skipper.ScaleRequest{}
	req.SetFunction(fn)
	req.SetDesiredInstances(desiredInstances)
	req.SetReason(reason)

	resp, err := c.client.Scale(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to scale: %w", err)
	}

	return resp.GetInstances(), nil
}

func (c *client) ReleaseInstance(ctx context.Context, inst *skipper.Instance) error {
	req := &skipper.ReleaseInstanceRequest{}
	req.SetInstance(inst)

	_, err := c.client.ReleaseInstance(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to release instance: %w", err)
	}

	return nil
}

// Close closes the connection.
func (c *client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
