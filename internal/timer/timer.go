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
	start := time.Now()
	for {
		if time.Since(start) >= timeout {
			return nil, fmt.Errorf("poll timed out after %v", timeout)
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
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
	for {
		timer := time.NewTimer(interval + time.Duration(rand.Int63n(2*int64(JITTER))) - JITTER)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
			err := fn(ctx)
			if err != nil {
				return err
			}
		}
	}
}
