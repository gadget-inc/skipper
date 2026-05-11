package router

import (
	"context"
	"errors"

	"github.com/gadget-inc/skipper/internal/skipper"
)

type assignmentCtxKey struct{}

func assignmentFromContext(ctx context.Context) (*skipper.Assignment, error) {
	a, ok := ctx.Value(assignmentCtxKey{}).(*skipper.Assignment)
	if !ok {
		return nil, errors.New("assignment not found in context")
	}
	return a, nil
}

func withAssignment(ctx context.Context, a *skipper.Assignment) context.Context {
	return context.WithValue(ctx, assignmentCtxKey{}, a)
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
