package controller

import (
	"context"
	"math/rand"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	"github.com/goccy/go-json"
	"go.opentelemetry.io/otel/trace"
)

func (c *Controller) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	switch req.URL.Path {
	case "/healthz":
		rw.WriteHeader(http.StatusOK)
	case "/instance":
		c.handleInstance(rw, req)
	case "/scale":
		c.handleScale(rw, req)
	case "/heartbeat":
		c.handleHeartbeat(rw, req)
	default:
		http.NotFound(rw, req)
	}
}

func (c *Controller) handleInstance(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	fn, err := function.FromHeader(req)
	if err != nil {
		log.Error(ctx, "failed to get function from header", key.Error.Field(err))
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	instances, err := c.getInstances(fn)
	if err != nil {
		log.Error(ctx, "failed to get instances", key.Error.Field(err), key.Function.Field(fn))
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}

	for len(instances) == 0 {
		instances, err = c.scaleFunction(ctx, fn, 1)
		if err != nil {
			log.Error(ctx, "failed to scale function", key.Error.Field(err), key.Function.Field(fn))
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	instance := instances[rand.Intn(len(instances))]

	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(http.StatusOK)
	err = json.NewEncoder(rw).Encode(instance)
	if err != nil {
		log.Error(ctx, "failed to encode get response", key.Error.Field(err))
	}
}

func (c *Controller) handleScale(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	fn, err := function.FromHeader(req)
	if err != nil {
		log.Error(ctx, "failed to get function from header", key.Error.Field(err))
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(key.Function.Attributes(fn)...)

	desiredInstances, err := strconv.Atoi(req.Header.Get(key.DesiredInstances.Header))
	if err != nil {
		log.Error(ctx, "failed to get desired instances from header", key.Error.Field(err))
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	instances, err := c.scaleFunction(ctx, fn, desiredInstances)
	if err != nil {
		log.Error(ctx, "failed to scale function", key.Error.Field(err), key.Function.Field(fn))
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}

	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(http.StatusOK)
	err = json.NewEncoder(rw).Encode(instances)
	if err != nil {
		log.Error(ctx, "failed to encode scale response", key.Error.Field(err))
	}
}

func (c *Controller) handleHeartbeat(rw http.ResponseWriter, req *http.Request) {
	var heartbeats []function.Heartbeat
	err := json.NewDecoder(req.Body).Decode(&heartbeats)
	if err != nil {
		log.Error(req.Context(), "failed to decode heartbeats", key.Error.Field(err))
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	c.heartbeatsMu.Lock()
	for _, heartbeat := range heartbeats {
		timestamp, ok := c.heartbeats[heartbeat.Function]
		if !ok || heartbeat.Timestamp.After(timestamp) {
			c.heartbeats[heartbeat.Function] = heartbeat.Timestamp
		}
	}
	c.heartbeatsMu.Unlock()

	log.Trace(req.Context(), "received heartbeats", key.Count.Field(len(heartbeats)))
	rw.WriteHeader(http.StatusOK)

	forwardedFor := req.Header.Values(key.ForwardedFor.Header)
	forwardedFor = append(forwardedFor, FlagIP.Value())

	for _, controllerIP := range c.ring.List() {
		if slices.Contains(forwardedFor, controllerIP) {
			continue
		}

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			err := c.getControllerClient(controllerIP).Heartbeat(ctx, heartbeats, forwardedFor...)
			if err != nil {
				log.Warn(req.Context(), "failed to forward heartbeats", key.Error.Field(err), key.ControllerIP.Field(controllerIP))
			}
		}()
	}
}
