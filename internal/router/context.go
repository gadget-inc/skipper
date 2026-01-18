package router

import (
	"context"
	"errors"

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
