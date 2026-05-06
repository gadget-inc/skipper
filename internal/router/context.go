package router

import (
	"context"
	"errors"

	"github.com/gadget-inc/skipper/internal/skipper"
)

type fnCtxKey struct{}

func assignmentFromContext(ctx context.Context) (*skipper.Assignment, error) {
	fn, ok := ctx.Value(fnCtxKey{}).(*skipper.Assignment)
	if !ok {
		return nil, errors.New("function not found in context")
	}
	return fn, nil
}

func withAssignment(ctx context.Context, fn *skipper.Assignment) context.Context {
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
