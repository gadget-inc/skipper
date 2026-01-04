package function

import (
	"context"
	"errors"
)

type ctxKey struct{}

var k = ctxKey{}

func From(ctx context.Context) (*Function, error) {
	fn, ok := ctx.Value(k).(*Function)
	if !ok {
		return nil, errors.New("function not found in context")
	}
	return fn, nil
}

func With(ctx context.Context, fn *Function) context.Context {
	return context.WithValue(ctx, k, fn)
}
