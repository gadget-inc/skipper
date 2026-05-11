package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gadget-inc/skipper/internal/skipper"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/resolver"
)

// Client is the interface for communicating with a controller.
type Client interface {
	Instance(ctx context.Context, a *skipper.Assignment, excludeInstanceNames ...string) (instance *skipper.Instance, err error)
	Heartbeat(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error
	Scale(ctx context.Context, a *skipper.Assignment, desiredInstances uint32, reason skipper.ScaleReason) ([]*skipper.Instance, error)
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

// grpcServiceName is the fully-qualified protobuf service name the
// retry policies attach to.
const grpcServiceName = "skipper.ControllerService"

// RetryPolicy describes the gRPC retry behavior for one RPC method.
// The slice [DefaultRetryPolicies] is the source of truth that both
// the gRPC client (via [defaultServiceConfig]) and the docs site read
// from.
type RetryPolicy struct {
	Method               string
	MaxAttempts          int
	InitialBackoff       time.Duration
	MaxBackoff           time.Duration
	BackoffMultiplier    int
	RetryableStatusCodes []string
}

// DefaultRetryPolicies are the per-RPC retry configurations the
// controller's gRPC client installs. GetInstance and Scale retry on
// UNAVAILABLE because routing and scaling are critical; Heartbeat
// retries to prevent premature pod termination; ReleaseInstance
// retries because the operation is idempotent.
var DefaultRetryPolicies = []RetryPolicy{
	{Method: "GetInstance", MaxAttempts: 3, InitialBackoff: 10 * time.Millisecond, MaxBackoff: 100 * time.Millisecond, BackoffMultiplier: 2, RetryableStatusCodes: []string{"UNAVAILABLE"}},
	{Method: "Heartbeat", MaxAttempts: 2, InitialBackoff: 10 * time.Millisecond, MaxBackoff: 50 * time.Millisecond, BackoffMultiplier: 2, RetryableStatusCodes: []string{"UNAVAILABLE"}},
	{Method: "Scale", MaxAttempts: 3, InitialBackoff: 50 * time.Millisecond, MaxBackoff: 500 * time.Millisecond, BackoffMultiplier: 2, RetryableStatusCodes: []string{"UNAVAILABLE"}},
	{Method: "ReleaseInstance", MaxAttempts: 2, InitialBackoff: 10 * time.Millisecond, MaxBackoff: 50 * time.Millisecond, BackoffMultiplier: 2, RetryableStatusCodes: []string{"UNAVAILABLE"}},
}

// defaultServiceConfig is the gRPC service-config JSON installed on
// every controller client. It is rendered from [DefaultRetryPolicies]
// at init time so the runtime config and the documented table cannot
// diverge.
var defaultServiceConfig = mustBuildServiceConfig(DefaultRetryPolicies)

func mustBuildServiceConfig(policies []RetryPolicy) string {
	type retryPolicyJSON struct {
		MaxAttempts          int      `json:"maxAttempts"`
		InitialBackoff       string   `json:"initialBackoff"`
		MaxBackoff           string   `json:"maxBackoff"`
		BackoffMultiplier    int      `json:"backoffMultiplier"`
		RetryableStatusCodes []string `json:"retryableStatusCodes"`
	}
	type methodNameJSON struct {
		Service string `json:"service"`
		Method  string `json:"method"`
	}
	type methodConfigJSON struct {
		Name        []methodNameJSON `json:"name"`
		RetryPolicy retryPolicyJSON  `json:"retryPolicy"`
	}
	type loadBalancingJSON struct {
		RoundRobin map[string]any `json:"round_robin"`
	}
	type serviceConfigJSON struct {
		LoadBalancingConfig []loadBalancingJSON `json:"loadBalancingConfig"`
		MethodConfig        []methodConfigJSON  `json:"methodConfig"`
	}

	cfg := serviceConfigJSON{
		LoadBalancingConfig: []loadBalancingJSON{{RoundRobin: map[string]any{}}},
		MethodConfig:        make([]methodConfigJSON, len(policies)),
	}
	for i, p := range policies {
		cfg.MethodConfig[i] = methodConfigJSON{
			Name: []methodNameJSON{{Service: grpcServiceName, Method: p.Method}},
			RetryPolicy: retryPolicyJSON{
				MaxAttempts:          p.MaxAttempts,
				InitialBackoff:       formatBackoff(p.InitialBackoff),
				MaxBackoff:           formatBackoff(p.MaxBackoff),
				BackoffMultiplier:    p.BackoffMultiplier,
				RetryableStatusCodes: p.RetryableStatusCodes,
			},
		}
	}
	out, err := json.Marshal(cfg)
	if err != nil {
		panic(fmt.Errorf("marshal service config: %w", err))
	}
	return string(out)
}

// formatBackoff renders d as the seconds-with-unit string the gRPC
// service-config schema accepts (e.g. "0.05s", "0.5s").
func formatBackoff(d time.Duration) string {
	return fmt.Sprintf("%gs", d.Seconds())
}

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

// NewClientWithResolver creates a new client using a custom gRPC resolver.Builder
// for service discovery. This enables zone-aware routing via EndpointSlice-based
// resolution instead of DNS.
func NewClientWithResolver(builder resolver.Builder) (Client, error) {
	target := fmt.Sprintf("%s:///controller", builder.Scheme())
	conn, err := grpc.NewClient(target,
		grpc.WithResolvers(builder),
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

func (c *client) Instance(ctx context.Context, a *skipper.Assignment, excludeInstanceNames ...string) (*skipper.Instance, error) {
	req := &skipper.GetInstanceRequest{}
	req.SetAssignment(a)
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

func (c *client) Scale(ctx context.Context, a *skipper.Assignment, desiredInstances uint32, reason skipper.ScaleReason) ([]*skipper.Instance, error) {
	req := &skipper.ScaleRequest{}
	req.SetAssignment(a)
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
