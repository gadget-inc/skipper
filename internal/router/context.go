package router

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/gadget-inc/skipper/internal/skipper"
)

type fnCtxKey struct{}

func functionFromContext(ctx context.Context) (*skipper.Function, error) {
	fn, ok := ctx.Value(fnCtxKey{}).(*skipper.Function)
	if !ok {
		return nil, errors.New("function not found in context")
	}
	return fn, nil
}

func withFunction(ctx context.Context, fn *skipper.Function) context.Context {
	return context.WithValue(ctx, fnCtxKey{}, fn)
}

// instanceResult is a mutable container placed in the context so that
// RoundTrip can pass the assigned instance back to ServeHTTP.
// This side-channel exists because httputil.ReverseProxy calls RoundTrip
// internally, and RoundTrip's fixed signature (*http.Response, error) provides
// no way to return the instance directly.
type instanceResult struct {
	instance *skipper.Instance
}

type instResultCtxKey struct{}

func withInstanceResult(ctx context.Context, r *instanceResult) context.Context {
	return context.WithValue(ctx, instResultCtxKey{}, r)
}

func instanceResultFromContext(ctx context.Context) *instanceResult {
	r, _ := ctx.Value(instResultCtxKey{}).(*instanceResult)
	return r
}

// inFlightTracker counts a request against a function's heartbeat only after
// it has acquired an instance. Counting earlier — while the request is still
// queued waiting on the controller — would advertise queue depth as demand
// and feed a positive loop: slow assignment inflates the in-flight gauge,
// the controller scales up, contention worsens, queue grows further.
type inFlightTracker struct {
	state   *heartbeatState
	fn      *skipper.Function
	counted atomic.Bool
}

func (t *inFlightTracker) markActive() {
	if t == nil || !t.counted.CompareAndSwap(false, true) {
		return
	}
	t.state.inFlight.Add(1)
	requestsInFlight.WithLabelValues(t.fn.GetDeployment()).Inc()
}

func (t *inFlightTracker) release() {
	if t == nil || !t.counted.CompareAndSwap(true, false) {
		return
	}
	t.state.inFlight.Add(-1)
	requestsInFlight.WithLabelValues(t.fn.GetDeployment()).Dec()
}

type inFlightTrackerCtxKey struct{}

func withInFlightTracker(ctx context.Context, t *inFlightTracker) context.Context {
	return context.WithValue(ctx, inFlightTrackerCtxKey{}, t)
}

func inFlightTrackerFromContext(ctx context.Context) *inFlightTracker {
	t, _ := ctx.Value(inFlightTrackerCtxKey{}).(*inFlightTracker)
	return t
}
