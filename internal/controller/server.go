package controller

import (
	"context"
	"math/rand"
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
	"github.com/puzpuzpuz/xsync/v4"
	"go.opentelemetry.io/otel/attribute"
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
		log.Error(ctx, "failed to get function from header", key.Error.Field(err))
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	ctx = log.With(ctx, key.Function.Field(fn))
	ctx = telemetry.WithPropagatedAttributes(ctx, key.Function.Attributes(fn)...)

	instances, err := ctrl.getReadyInstances(fn)
	if err != nil {
		log.Error(ctx, "failed to get instances", key.Error.Field(err))
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}

	telemetry.SetAttributes(ctx, attribute.Bool("has_instances", len(instances) > 0))

	for len(instances) == 0 {
		if instances, err = ctrl.scale(ctx, fn, ScalingDecision{
			DesiredInstances:          1,
			UnclampedDesiredInstances: 1,
			Reason:                    "no ready instances",
		}); err != nil {
			log.Error(ctx, "failed to scale function", key.Error.Field(err))
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if len(instances) > fn.Scale.MaxInstances {
		// sort instances by assigned at in descending order (newest first)
		slices.SortFunc(instances, func(a, b *function.Instance) int { return b.AssignedAt.Compare(a.AssignedAt) })
		// keep the newest instances
		instances = instances[:fn.Scale.MaxInstances]
	}

	filteredInstances := instances
	// Optionally exclude instances by name if provided by the router
	if excludeHeader := req.Header.Get("X-Exclude-Instance-Names"); excludeHeader != "" && len(instances) > 0 {
		exclude := map[string]struct{}{}
		for _, name := range strings.Split(excludeHeader, ",") {
			if name == "" {
				continue
			}
			exclude[strings.TrimSpace(name)] = struct{}{}
		}
		if len(exclude) > 0 {
			filteredInstances = instances[:0]
			for _, inst := range instances {
				if _, found := exclude[inst.Name]; !found {
					filteredInstances = append(filteredInstances, inst)
				}
			}
		}
	}

	if len(filteredInstances) == 0 {
		log.Error(ctx, "no instances available")
		http.Error(rw, "no instances available", http.StatusInternalServerError)
		return
	} else {
		instances = filteredInstances
	}

	instance := instances[rand.Intn(len(instances))]

	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(http.StatusOK)
	if err := json.MarshalWrite(rw, instance); err != nil {
		log.Error(ctx, "failed to encode instance response", key.Error.Field(err))
	}
}

func (ctrl *Controller) handleScale(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	fn, err := function.FromHeader(req)
	if err != nil {
		log.Error(ctx, "failed to get function from header", key.Error.Field(err))
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	ctx = log.With(ctx, key.Function.Field(fn))
	ctx = telemetry.WithPropagatedAttributes(ctx, key.Function.Attributes(fn)...)

	desiredInstances, err := strconv.Atoi(req.Header.Get(key.DesiredInstances.Header))
	if err != nil {
		log.Error(ctx, "failed to get desired instances from header", key.Error.Field(err))
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	reason := req.Header.Get(key.Reason.Header)
	if reason == "" {
		reason = "unknown for forwarded request"
	}

	instances, err := ctrl.scale(ctx, fn, ScalingDecision{
		DesiredInstances: desiredInstances,
		Reason:           reason,
	})
	if err != nil {
		log.Error(ctx, "failed to scale function", key.Error.Field(err))
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}

	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(http.StatusOK)
	if err := json.MarshalWrite(rw, instances); err != nil {
		log.Error(ctx, "failed to encode scale response", key.Error.Field(err))
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
		log.Error(req.Context(), "failed to decode heartbeats", key.Error.Field(err))
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	for _, heartbeat := range heartbeats {
		heartbeatsCounter.WithLabelValues(heartbeat.Function.Deployment).Inc()

		ctrl.routerHeartbeats.Compute(heartbeat.Function, func(routerHeartbeats RouterHeartbeats, loaded bool) (RouterHeartbeats, xsync.ComputeOp) {
			if !loaded {
				routerHeartbeats = make(RouterHeartbeats)
			}
			if routerHeartbeats[routerIP].Timestamp.Before(heartbeat.Timestamp) {
				routerHeartbeats[routerIP] = heartbeat
			}
			// garbage collect router heartbeats that haven't been updated in the timeout period
			for routerIP := range routerHeartbeats {
				if time.Since(routerHeartbeats[routerIP].Timestamp) > FlagHeartbeatTimeout.Value() {
					delete(routerHeartbeats, routerIP)
				}
			}
			return routerHeartbeats, xsync.UpdateOp
		})
	}

	log.Trace(req.Context(), "received heartbeats", key.Count.Field(len(heartbeats)))
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
				log.Warn(ctx, "failed to forward heartbeats", key.Error.Field(err), key.ResponsibleIP.Field(controllerIP))
			}
		}()
	}
}
