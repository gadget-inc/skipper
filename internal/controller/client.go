package controller

import (
	"context"

	"github.com/gadget-inc/skipper/internal/function"
)

type Client interface {
	Instance(ctx context.Context, fn *function.Function) (instance *function.Instance, err error)
	Heartbeat(ctx context.Context, routerIP string, heartbeats []*function.Heartbeat, forwardedFor ...string) error
	Scale(ctx context.Context, fn *function.Function, desiredInstances int, reason string) ([]*function.Instance, error)
}

type NewClientFunc func(host string, port int) Client
