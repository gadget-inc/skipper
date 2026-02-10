package controller

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/gadget-inc/skipper/internal/telemetry"
	"github.com/go-json-experiment/json"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var heartbeatsCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "skipper",
	Subsystem: "controller",
	Name:      "heartbeats_total",
	Help:      "The number of heartbeats received by the controller",
}, []string{"function_deployment"})

func (ctrl *Controller) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", ctrl.handleHealthz)
	mux.HandleFunc("GET /instance", ctrl.handleInstance)
	mux.HandleFunc("POST /scale", ctrl.handleScale)
	mux.HandleFunc("POST /heartbeat", ctrl.handleHeartbeat)
	mux.HandleFunc("POST /replace-instance", ctrl.handleReplaceInstance)
	mux.Handle("/", http.NotFoundHandler())

	return mux
}

func (ctrl *Controller) handleHealthz(rw http.ResponseWriter, req *http.Request) {
	if !ctrl.Ready() {
		rw.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	rw.WriteHeader(http.StatusOK)
}

func (ctrl *Controller) handleInstance(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	fn, err := skipper.FunctionFromHeader(req)
	if err != nil {
		log.Error(ctx, "failed to get function from header", key.Error.Slog(err))
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	ctx = telemetry.With(ctx, key.Function.Attr(fn))

	var excludeNames []string
	if excludeHeader := req.Header.Get(key.ExcludeInstanceNames.Header); excludeHeader != "" {
		excludeNames = strings.Split(excludeHeader, ",")
	}

	instance, err := ctrl.supervisor(fn).getReadyInstance(ctx, excludeNames)
	if err != nil {
		log.Error(ctx, "failed to get ready instance", key.Error.Slog(err))
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}

	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(http.StatusOK)
	if err := json.MarshalWrite(rw, instance); err != nil {
		log.Error(ctx, "failed to encode instance response", key.Error.Slog(err))
	}
}

func (ctrl *Controller) handleScale(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	fn, err := skipper.FunctionFromHeader(req)
	if err != nil {
		log.Error(ctx, "failed to get function from header", key.Error.Slog(err))
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	ctx = telemetry.With(ctx, key.Function.Attr(fn))

	desiredInstances64, err := strconv.ParseUint(req.Header.Get(key.DesiredInstances.Header), 10, 32)
	if err != nil {
		log.Error(ctx, "failed to get desired instances from header", key.Error.Slog(err))
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	desiredInstances := uint32(desiredInstances64)

	reasonStr := req.Header.Get(key.Reason.Header)
	reason, ok := skipper.ScaleReason_value[reasonStr]
	if !ok {
		log.Warn(ctx, "invalid scaling reason, using unspecified", key.Reason.Slog(reasonStr))
		reason = int32(skipper.ScaleReason_SCALE_REASON_UNSPECIFIED)
	}

	decision := &skipper.ScaleDecision{}
	decision.SetDesiredInstances(desiredInstances)
	decision.SetReason(skipper.ScaleReason(reason))
	instances, err := ctrl.supervisor(fn).scale(ctx, decision)
	if err != nil {
		log.Error(ctx, "failed to scale function", key.Error.Slog(err))
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}

	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(http.StatusOK)
	if err := json.MarshalWrite(rw, instances); err != nil {
		log.Error(ctx, "failed to encode scale response", key.Error.Slog(err))
	}
}

func (ctrl *Controller) handleReplaceInstance(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	fn, err := skipper.FunctionFromHeader(req)
	if err != nil {
		log.Error(ctx, "failed to get function from header", key.Error.Slog(err))
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	instanceName := req.Header.Get(key.InstanceName.Header)
	if instanceName == "" {
		log.Error(ctx, "missing instance name header")
		http.Error(rw, "missing "+key.InstanceName.Header, http.StatusBadRequest)
		return
	}

	ctx = telemetry.With(ctx, key.Function.Attr(fn), key.InstanceName.Attr(instanceName))
	log.Info(ctx, "marking instance as application-stale")

	ctrl.supervisor(fn).markInstanceStale(ctx, instanceName)
	rw.WriteHeader(http.StatusOK)
}

func (ctrl *Controller) handleHeartbeat(rw http.ResponseWriter, req *http.Request) {
	routerIP := req.Header.Get(key.RouterIP.Header)
	if routerIP == "" {
		log.Error(req.Context(), "failed to get router IP from header")
		http.Error(rw, "missing "+key.RouterIP.Header, http.StatusBadRequest)
		return
	}

	var heartbeats []*skipper.Heartbeat
	if err := json.UnmarshalRead(req.Body, &heartbeats); err != nil {
		log.Error(req.Context(), "failed to decode heartbeats", key.Error.Slog(err))
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	for _, heartbeat := range heartbeats {
		heartbeatsCounter.WithLabelValues(heartbeat.GetFunction().GetDeployment()).Inc()
		ctrl.supervisor(heartbeat.GetFunction()).heartbeat(routerIP, heartbeat)
	}

	log.Trace(req.Context(), "received heartbeats", key.Count.Slog(len(heartbeats)))
	rw.WriteHeader(http.StatusOK)

	controllersThatHaveReceivedHeartbeats := slices.Clone(req.Header[key.ForwardedFor.Header])
	controllersThatHaveReceivedHeartbeats = append(controllersThatHaveReceivedHeartbeats, ctrl.config.PodIP)

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
			ctx, cancel := context.WithTimeout(context.WithoutCancel(req.Context()), 5*time.Second)
			defer cancel()

			if err := ctrl.getControllerClient(controllerIP).Heartbeat(ctx, routerIP, heartbeats, forwardedFor...); err != nil {
				log.Warn(ctx, "failed to forward heartbeats", key.Error.Slog(err), key.ResponsibleIP.Slog(controllerIP))
			}
		}()
	}
}
