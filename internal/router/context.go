package router

import (
	"context"
	"errors"

	"github.com/gadget-inc/skipper/internal/function"
)

type fnCtxKey struct{}

func functionFromContext(ctx context.Context) (*function.Function, error) {
	fn, ok := ctx.Value(fnCtxKey{}).(*function.Function)
	if !ok {
		return nil, errors.New("function not found in context")
	}
	return fn, nil
}

func withFunction(ctx context.Context, fn *function.Function) context.Context {
	return context.WithValue(ctx, fnCtxKey{}, fn)
}
