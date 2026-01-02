package controller

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gadget-inc/skipper/internal/function"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
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
	mux.Handle("/", http.NotFoundHandler())

	return mux
}

func (ctrl *Controller) handleHealthz(rw http.ResponseWriter, req *http.Request) {
	rw.WriteHeader(http.StatusOK)
}

func (ctrl *Controller) handleInstance(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	fn, err := function.FromHeader(req)
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
	fn, err := function.FromHeader(req)
	if err != nil {
		log.Error(ctx, "failed to get function from header", key.Error.Slog(err))
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	ctx = telemetry.With(ctx, key.Function.Attr(fn))

	desiredInstances, err := strconv.Atoi(req.Header.Get(key.DesiredInstances.Header))
	if err != nil {
		log.Error(ctx, "failed to get desired instances from header", key.Error.Slog(err))
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	reason := req.Header.Get(key.Reason.Header)
	if !isValidScalingReason(reason) {
		log.Warn(ctx, "invalid scaling reason, using unknown", key.Reason.Slog(reason))
		reason = ScalingReasonUnknown
	}

	instances, err := ctrl.supervisor(fn).scale(ctx, ScalingDecision{
		DesiredInstances: desiredInstances,
		Reason:           reason,
	})
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

func (ctrl *Controller) handleHeartbeat(rw http.ResponseWriter, req *http.Request) {
	routerIP := req.Header.Get(key.RouterIP.Header)
	if routerIP == "" {
		log.Error(req.Context(), "failed to get router IP from header")
		http.Error(rw, "missing "+key.RouterIP.Header, http.StatusBadRequest)
		return
	}

	var heartbeats []function.Heartbeat
	if err := json.UnmarshalRead(req.Body, &heartbeats); err != nil {
		log.Error(req.Context(), "failed to decode heartbeats", key.Error.Slog(err))
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	for _, heartbeat := range heartbeats {
		heartbeatsCounter.WithLabelValues(heartbeat.Function.Deployment).Inc()
		ctrl.supervisor(heartbeat.Function).heartbeat(routerIP, heartbeat)
	}

	log.Trace(req.Context(), "received heartbeats", key.Count.Slog(len(heartbeats)))
	rw.WriteHeader(http.StatusOK)

	controllersThatHaveReceivedHeartbeats := slices.Clone(req.Header[key.ForwardedFor.Header])
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
			ctx, cancel := context.WithTimeout(context.WithoutCancel(req.Context()), 5*time.Second)
			defer cancel()

			if err := ctrl.getControllerClient(controllerIP).Heartbeat(ctx, routerIP, heartbeats, forwardedFor...); err != nil {
				log.Warn(ctx, "failed to forward heartbeats", key.Error.Slog(err), key.ResponsibleIP.Slog(controllerIP))
			}
		}()
	}
}
