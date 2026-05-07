package controller

import (
	"context"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/gadget-inc/skipper/internal/telemetry"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var heartbeatsCounter = metrics.NewCounterVec(prometheus.CounterOpts{
	Namespace: "skipper",
	Subsystem: "controller",
	Name:      "heartbeats_total",
	Help:      "Heartbeats received from routers.",
}, []string{"function_deployment", "assignment_deployment"})

// Server implements the ControllerServiceServer interface.
type Server struct {
	skipper.UnimplementedControllerServiceServer
	ctrl *Controller
}

// NewServer creates a new server wrapping the controller.
func NewServer(ctrl *Controller) *Server {
	return &Server{ctrl: ctrl}
}

func (s *Server) GetInstance(ctx context.Context, req *skipper.GetInstanceRequest) (*skipper.GetInstanceResponse, error) {
	fn := req.GetAssignment()
	if err := fn.Validate(); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid assignment: %v", err)
	}

	ctx = telemetry.With(ctx, skipper.LegacyFunctionKey.Attr(fn), skipper.AssignmentKey.Attr(fn))

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

func (s *Server) Heartbeat(ctx context.Context, req *skipper.HeartbeatRequest) (*skipper.HeartbeatResponse, error) {
	routerIP := req.GetRouterIp()
	if routerIP == "" {
		return nil, status.Error(codes.InvalidArgument, "missing router_ip")
	}

	heartbeats := req.GetHeartbeats()
	for _, heartbeat := range heartbeats {
		deployment := heartbeat.GetAssignment().GetDeployment()
		heartbeatsCounter.WithLabelValues(deployment, deployment).Inc()
		s.ctrl.supervisor(heartbeat.GetAssignment()).heartbeat(routerIP, heartbeat)
	}

	log.Trace(ctx, "received heartbeats", key.Count.Slog(len(heartbeats)))

	// Forward heartbeats to other controllers
	targets, forwardedFor := heartbeatTargets(s.ctrl.ring.List(), req.GetForwardedFor(), s.ctrl.config.PodIP)

	for _, controllerIP := range targets {
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

func (s *Server) ReleaseInstance(ctx context.Context, req *skipper.ReleaseInstanceRequest) (*skipper.ReleaseInstanceResponse, error) {
	inst := req.GetInstance()
	name := inst.GetName()
	namespace := inst.GetAssignment().GetNamespace()
	if name == "" || namespace == "" {
		return nil, status.Error(codes.InvalidArgument, "missing instance name or namespace")
	}

	ctx = telemetry.With(ctx, skipper.InstanceKey.Attr(inst))

	err := s.ctrl.deletePod(ctx, namespace, name, metav1.DeleteOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return &skipper.ReleaseInstanceResponse{}, nil
		}
		log.Error(ctx, "failed to release instance", key.Error.Slog(err))
		return nil, status.Errorf(codes.Internal, "failed to release instance: %v", err)
	}

	log.Info(ctx, "released instance")
	return &skipper.ReleaseInstanceResponse{}, nil
}

func (s *Server) GetClusterState(ctx context.Context, _ *skipper.GetClusterStateRequest) (*skipper.GetClusterStateResponse, error) {
	state := s.ctrl.ClusterState(ctx)
	resp := &skipper.GetClusterStateResponse{}
	resp.SetClusterState(state)
	return resp, nil
}

func (s *Server) Scale(ctx context.Context, req *skipper.ScaleRequest) (*skipper.ScaleResponse, error) {
	fn := req.GetAssignment()
	if err := fn.Validate(); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid assignment: %v", err)
	}

	ctx = telemetry.With(ctx, skipper.LegacyFunctionKey.Attr(fn), skipper.AssignmentKey.Attr(fn))

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
