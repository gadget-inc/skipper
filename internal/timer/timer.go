package timer

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

// Poll polls the given function until it returns a non-nil result or the timeout is reached.
func Poll[T any](ctx context.Context, interval, timeout time.Duration, fn func(context.Context) (*T, error)) (*T, error) {
	result, err := fn(ctx)
	if err != nil {
		return nil, err
	}
	if result != nil {
		return result, nil
	}

	start := time.Now()
	tick := time.Tick(interval)
	for {
		if time.Since(start) >= timeout {
			return nil, fmt.Errorf("poll timed out after %v", timeout)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-tick:
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

// PollUntil polls the given function until it returns a non-nil result or the context is cancelled.
func PollUntil[T any](ctx context.Context, interval time.Duration, fn func(context.Context) (*T, error)) (*T, error) {
	if ctx.Done() == nil {
		panic("timer.PollUntil context must be cancellable")
	}
	return Poll(ctx, interval, time.Duration(1<<63-1), fn)
}

// Loop calls the given function at the given interval until the it returns an error or the context is cancelled.
func Loop(ctx context.Context, interval time.Duration, fn func(context.Context) error) error {
	if ctx.Done() == nil {
		panic("timer.Loop context must be cancellable")
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
