package controller

import (
	"context"
	"slices"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/gadget-inc/skipper/internal/telemetry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GRPCServer implements the ControllerServiceServer interface.
type GRPCServer struct {
	skipper.UnimplementedControllerServiceServer
	ctrl *Controller
}

// NewGRPCServer creates a new gRPC server wrapping the controller.
func NewGRPCServer(ctrl *Controller) *GRPCServer {
	return &GRPCServer{ctrl: ctrl}
}

func (s *GRPCServer) GetInstance(ctx context.Context, req *skipper.GetInstanceRequest) (*skipper.GetInstanceResponse, error) {
	fn := req.GetFunction()
	if err := fn.Validate(); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid function: %v", err)
	}

	ctx = telemetry.With(ctx, key.Function.Attr(fn))

	excludeNames := req.GetExcludeInstanceNames()

	instance, err := s.ctrl.supervisor(fn).getReadyInstance(ctx, excludeNames)
	if err != nil {
		log.Error(ctx, "failed to get ready instance", key.Error.Slog(err))
		return nil, status.Errorf(codes.Internal, "failed to get ready instance: %v", err)
	}

	resp := &skipper.GetInstanceResponse{}
	resp.SetInstance(instance)
	return resp, nil
}

func (s *GRPCServer) Heartbeat(ctx context.Context, req *skipper.HeartbeatRequest) (*skipper.HeartbeatResponse, error) {
	routerIP := req.GetRouterIp()
	if routerIP == "" {
		return nil, status.Error(codes.InvalidArgument, "missing router_ip")
	}

	heartbeats := req.GetHeartbeats()
	for _, heartbeat := range heartbeats {
		heartbeatsCounter.WithLabelValues(heartbeat.GetFunction().GetDeployment()).Inc()
		s.ctrl.supervisor(heartbeat.GetFunction()).heartbeat(routerIP, heartbeat)
	}

	log.Trace(ctx, "received heartbeats", key.Count.Slog(len(heartbeats)))

	// Forward heartbeats to other controllers
	controllersThatHaveReceivedHeartbeats := slices.Clone(req.GetForwardedFor())
	controllersThatHaveReceivedHeartbeats = append(controllersThatHaveReceivedHeartbeats, s.ctrl.config.PodIP)

	var controllersThatWillReceiveHeartbeats []string
	for _, controllerIP := range s.ctrl.ring.List() {
		if !slices.Contains(controllersThatHaveReceivedHeartbeats, controllerIP) {
			controllersThatWillReceiveHeartbeats = append(controllersThatWillReceiveHeartbeats, controllerIP)
		}
	}

	forwardedFor := append(controllersThatHaveReceivedHeartbeats, controllersThatWillReceiveHeartbeats...)

	for _, controllerIP := range controllersThatWillReceiveHeartbeats {
		go func() {
			forwardCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()

			if err := s.ctrl.getControllerClient(controllerIP).Heartbeat(forwardCtx, routerIP, heartbeats, forwardedFor...); err != nil {
				log.Warn(forwardCtx, "failed to forward heartbeats", key.Error.Slog(err), key.ResponsibleIP.Slog(controllerIP))
			}
		}()
	}

	return &skipper.HeartbeatResponse{}, nil
}

func (s *GRPCServer) Scale(ctx context.Context, req *skipper.ScaleRequest) (*skipper.ScaleResponse, error) {
	fn := req.GetFunction()
	if err := fn.Validate(); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid function: %v", err)
	}

	ctx = telemetry.With(ctx, key.Function.Attr(fn))

	desiredInstances := req.GetDesiredInstances()
	reason := req.GetReason()

	decision := &skipper.ScaleDecision{}
	decision.SetDesiredInstances(desiredInstances)
	decision.SetReason(reason)

	instances, err := s.ctrl.supervisor(fn).scale(ctx, decision)
	if err != nil {
		log.Error(ctx, "failed to scale function", key.Error.Slog(err))
		return nil, status.Errorf(codes.Internal, "failed to scale function: %v", err)
	}

	resp := &skipper.ScaleResponse{}
	resp.SetInstances(instances)
	return resp, nil
}
