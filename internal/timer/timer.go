package timer

import (
	"context"
	"math/rand"
	"time"
)

// Poll calls the given function until it returns a non-nil result or the context is cancelled
func Poll[T any](ctx context.Context, interval time.Duration, fn func(context.Context) (*T, error)) (*T, error) {
	if ctx.Done() == nil {
		panic("timer.Poll context must be cancellable")
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	result, err := fn(ctx)
	if err != nil {
		return nil, err
	}
	if result != nil {
		return result, nil
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(addJitter(interval)):
			result, err := fn(ctx)
			if err != nil {
				return nil, err
			}
			if result != nil {
				return result, nil
			}
		}
	}
}

// Loop calls the given function at the given interval until it returns an error or the context is cancelled.
func Loop(ctx context.Context, interval time.Duration, fn func(context.Context) error) error {
	if ctx.Done() == nil {
		panic("timer.Loop context must be cancellable")
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	err := fn(ctx)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(addJitter(interval)):
			err := fn(ctx)
			if err != nil {
				return err
			}
		}
	}
}

func addJitter(interval time.Duration) time.Duration {
	const jitter = 200 * time.Millisecond
	return interval + time.Duration(rand.Int63n(2*int64(jitter))) - jitter
}
