package function

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("function not found in context")

type ctxKey struct{}

var k = ctxKey{}

func From(ctx context.Context) (Function, error) {
	fn, ok := ctx.Value(k).(Function)
	if !ok {
		return emptyFunction, ErrNotFound
	}
	return fn, nil
}

func With(ctx context.Context, fn Function) context.Context {
	return context.WithValue(ctx, k, fn)
}
