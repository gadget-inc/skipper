package timer

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

const (
	JITTER = 200 * time.Millisecond
)

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

func Loop(ctx context.Context, interval time.Duration, fn func(context.Context) error) error {
	tick := time.Tick(interval + time.Duration(rand.Int63n(2*int64(JITTER))) - JITTER)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick:
			err := fn(ctx)
			if err != nil {
				return err
			}
		}
	}
}
