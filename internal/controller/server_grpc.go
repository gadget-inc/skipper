package controller

import (
	"context"
	"math/rand"
	"slices"
	"time"

	"github.com/gadget-inc/skipper/internal/function"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/telemetry"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/attribute"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/stats"
	status "google.golang.org/grpc/status"
)

func (ctrl *Controller) GRPCServer() *grpc.Server {
	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler(otelgrpc.WithFilter(func(stats *stats.RPCTagInfo) bool {
			return stats.FullMethodName != "/grpc.health.v1.Health/Check"
		}))),
	)

	RegisterControllerServer(grpcServer, ctrl)
	healthgrpc.RegisterHealthServer(grpcServer, health.NewServer())

	return grpcServer
}

func (ctrl *Controller) mustEmbedUnimplementedControllerServer() {}

func (ctrl *Controller) GetOrCreateInstance(ctx context.Context, req *GetOrCreateInstanceRequest) (*GetOrCreateInstanceResponse, error) {
	fn := req.GetFunction()

	ctx = log.With(ctx, key.Function.Field(fn))
	ctx = telemetry.WithPropagatedAttributes(ctx, key.Function.Attributes(fn)...)

	instances, err := ctrl.getReadyInstances(fn)
	if err != nil {
		log.Error(ctx, "failed to get instances", key.Error.Field(err))
		return nil, status.Errorf(codes.Internal, "failed to get instances: %v", err)
	}

	telemetry.SetAttributes(ctx, attribute.Bool("has_instances", len(instances) > 0))

	for len(instances) == 0 {
		if instances, err = ctrl.supervisor(fn).scale(ctx, ScalingDecision{
			DesiredInstances:          1,
			UnclampedDesiredInstances: 1,
			Reason:                    "no ready instances",
		}); err != nil {
			log.Error(ctx, "failed to scale function", key.Error.Field(err))
			return nil, status.Errorf(codes.Internal, "failed to scale function: %v", err)
		}
	}

	if len(instances) > int(fn.GetScale().GetMaxInstances()) {
		// sort instances by assigned at in descending order (newest first)
		slices.SortFunc(instances, func(a, b *function.Instance) int {
			return b.GetAssignedAt().AsTime().Compare(a.GetAssignedAt().AsTime())
		})

		// keep the newest instances
		instances = instances[:fn.GetScale().GetMaxInstances()]
	}

	response := new(GetOrCreateInstanceResponse)
	response.SetInstance(instances[rand.Intn(len(instances))])
	return response, nil
}

func (ctrl *Controller) Scale(ctx context.Context, req *ScaleRequest) (*ScaleResponse, error) {
	fn := req.GetFunction()

	ctx = log.With(ctx, key.Function.Field(fn))
	ctx = telemetry.WithPropagatedAttributes(ctx, key.Function.Attributes(fn)...)

	reason := req.GetReason()
	if reason == "" {
		reason = "unknown for forwarded request"
	}

	instances, err := ctrl.supervisor(fn).scale(ctx, ScalingDecision{
		DesiredInstances: int(req.GetDesiredInstances()),
		Reason:           reason,
	})
	if err != nil {
		log.Error(ctx, "failed to scale function", key.Error.Field(err))
		return nil, status.Errorf(codes.Internal, "failed to scale function: %v", err)
	}

	response := new(ScaleResponse)
	response.SetInstances(instances)
	return response, nil
}

func (ctrl *Controller) SendHeartbeats(ctx context.Context, req *SendHeartbeatsRequest) (*SendHeartbeatsResponse, error) {
	heartbeats := req.GetHeartbeats()

	for _, heartbeat := range heartbeats {
		heartbeatsCounter.WithLabelValues(heartbeat.GetFunction().GetDeployment()).Inc()
		ctrl.supervisor(heartbeat.GetFunction()).heartbeat(req.GetRouterIp(), heartbeat)
	}

	go func() {
		controllersThatHaveReceivedHeartbeats := slices.Clone(req.GetForwardedFor())
		controllersThatHaveReceivedHeartbeats = append(controllersThatHaveReceivedHeartbeats, FlagPodIP.Value())

		var controllersThatWillReceiveHeartbeats []string
		for _, controllerIP := range ctrl.ring.List() {
			if !slices.Contains(controllersThatHaveReceivedHeartbeats, controllerIP) {
				controllersThatWillReceiveHeartbeats = append(controllersThatWillReceiveHeartbeats, controllerIP)
			}
		}

		// make forwardedFor contain all controllers that have received heartbeats and all controllers that will receive heartbeats
		// this ensures that the heartbeats are forwarded to all the controllers once, and only once
		forwardedFor := append(controllersThatHaveReceivedHeartbeats, controllersThatWillReceiveHeartbeats...)

		for _, controllerIP := range controllersThatWillReceiveHeartbeats {
			go func() {
				ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer cancel()

				if err := ctrl.getControllerClient(controllerIP).Heartbeat(ctx, req.GetRouterIp(), heartbeats, forwardedFor...); err != nil {
					log.Warn(ctx, "failed to forward heartbeats", key.Error.Field(err), key.ResponsibleIP.Field(controllerIP))
				}
			}()
		}
	}()

	return new(SendHeartbeatsResponse), nil
}
