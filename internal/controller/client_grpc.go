package controller

import (
	"context"
	"fmt"

	"github.com/gadget-inc/skipper/internal/function"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type grpcClient struct {
	ControllerClient
}

var _ Client = &grpcClient{}

func NewGRPCClient(host string, port int) Client {
	conn, err := grpc.NewClient(fmt.Sprintf("%s:%d", host, port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		panic(fmt.Errorf("failed to create grpc client: %w", err))
	}
	return &grpcClient{ControllerClient: NewControllerClient(conn)}
}

// Instance implements Client.
func (g *grpcClient) Instance(ctx context.Context, fn *function.Function) (*function.Instance, error) {
	request := new(GetOrCreateInstanceRequest)
	request.SetFunction(fn)

	response, err := g.GetOrCreateInstance(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to get or create instance: %w", err)
	}

	return response.GetInstance(), nil
}

// Heartbeat implements Client.
func (g *grpcClient) Heartbeat(ctx context.Context, routerIP string, heartbeats []*function.Heartbeat, forwardedFor ...string) error {
	if len(heartbeats) == 0 {
		return nil
	}

	request := new(SendHeartbeatsRequest)
	request.SetRouterIp(routerIP)
	request.SetHeartbeats(heartbeats)
	request.SetForwardedFor(forwardedFor)

	_, err := g.SendHeartbeats(ctx, request)
	if err != nil {
		return fmt.Errorf("failed to send heartbeats: %w", err)
	}

	return nil
}

// Scale implements Client.
func (g *grpcClient) Scale(ctx context.Context, fn *function.Function, desiredInstances int, reason string) ([]*function.Instance, error) {
	request := new(ScaleRequest)
	request.SetFunction(fn)
	request.SetDesiredInstances(uint32(desiredInstances))
	request.SetReason(reason)

	response, err := g.ControllerClient.Scale(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to scale function: %w", err)
	}

	return response.GetInstances(), nil
}
