package router

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/gadget-inc/fusion/internal/buffer"
	"github.com/gadget-inc/fusion/internal/controller"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	"github.com/gadget-inc/fusion/internal/timer"
	"github.com/puzpuzpuz/xsync/v3"
)

type Router struct {
	controller   controller.Client
	heartbeats   *xsync.MapOf[function.Function, time.Time]
	reverseProxy *httputil.ReverseProxy
	transport    *http.Transport
}

func New(controllerClient controller.Client) *Router {
	r := &Router{
		controller: controllerClient,
		heartbeats: xsync.NewMapOf[function.Function, time.Time](),
		transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   2 * time.Second, // this is the only change from the default transport
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}

	r.reverseProxy = &httputil.ReverseProxy{
		BufferPool: buffer.Pool,
		Rewrite:    rewriteHeaders,
		Transport:  r,
	}

	return r
}

func (r *Router) Start(ctx context.Context) {
	go timer.Loop(ctx, FlagHeartbeatInterval.Value, func(ctx context.Context) error {
		var heartbeats []controller.Heartbeat
		r.heartbeats.Range(func(fn function.Function, timestamp time.Time) bool {
			r.heartbeats.Delete(fn)
			heartbeats = append(heartbeats, controller.Heartbeat{Function: fn, Timestamp: timestamp})
			return true
		})

		err := r.controller.Heartbeat(ctx, heartbeats)
		if err != nil {
			log.Warn(ctx, "failed to send heartbeats", key.Error.Field(err))
		}
		return nil
	})
}

func (r *Router) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	fn, err := function.FromHeaders(req)
	if err != nil {
		if req.Method == http.MethodGet && req.URL.Path == "/healthz" {
			rw.WriteHeader(http.StatusOK)
			return
		}
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	r.heartbeats.Store(fn, time.Now())
	go timer.Loop(req.Context(), FlagHeartbeatInterval.Value, func(ctx context.Context) error {
		r.heartbeats.Store(fn, time.Now())
		return nil
	})

	r.reverseProxy.ServeHTTP(rw, req.WithContext(function.With(req.Context(), fn)))
}

func (r *Router) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	fn, err := function.From(ctx)
	if err != nil {
		return nil, err
	}

	attempt := 0
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if attempt > FlagGetAttempts.Value {
			return nil, fmt.Errorf("failed to get a pod after %d attempts", attempt)
		}

		if attempt > 0 {
			time.Sleep(1 * time.Second * time.Duration(attempt))
		}

		instance, err := r.controller.Get(ctx, fn)
		if err != nil {
			log.Warn(ctx, "failed to get instance for function", key.Error.Field(err), key.Function.Field(fn))
			attempt++
			continue
		}

		req.URL.Scheme = "http"
		req.URL.Host = instance.Addr

		res, err := r.transport.RoundTrip(req)

		var netOpErr *net.OpError
		if errors.As(err, &netOpErr) {
			if netOpErr.Op == "dial" || netOpErr.Timeout() {
				log.Warn(ctx, "failed to connect to instance", key.Error.Field(err), key.Instance.Field(instance))
				attempt++
				continue
			}
		}

		if err != nil && err != context.Canceled {
			log.Error(ctx, "unknown error", key.Error.Field(err), key.Instance.Field(instance))
		}

		return res, err
	}
}

func rewriteHeaders(r *httputil.ProxyRequest) {
	function.RemoveHeaders(r.Out)
	r.Out.Host = r.In.Host
	r.Out.Header["X-Forwarded-For"] = r.In.Header["X-Forwarded-For"]
	r.SetXForwarded()
}
