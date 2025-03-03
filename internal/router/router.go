package router

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/gadget-inc/fusion/internal/controller"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	"github.com/gadget-inc/fusion/internal/timer"
	"github.com/puzpuzpuz/xsync/v3"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type Router struct {
	controller   controller.Client
	heartbeats   *xsync.MapOf[function.Function, time.Time]
	reverseProxy *httputil.ReverseProxy
	roundTripper http.RoundTripper
}

func New(controllerClient controller.Client) *Router {
	r := &Router{
		controller: controllerClient,
		heartbeats: xsync.NewMapOf[function.Function, time.Time](),
		roundTripper: otelhttp.NewTransport(&http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   2 * time.Second, // default is 30 seconds
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			DisableCompression:    true, // disable the Accept-Encoding header
		}),
	}

	r.reverseProxy = &httputil.ReverseProxy{
		BufferPool: bufferPool,
		Rewrite:    rewriteHeaders,
		Transport:  r,
	}

	return r
}

func (r *Router) Start(ctx context.Context) {
	go timer.Loop(ctx, FlagHeartbeatInterval.Value(), func(ctx context.Context) error {
		var heartbeats []function.Heartbeat
		r.heartbeats.Range(func(fn function.Function, timestamp time.Time) bool {
			r.heartbeats.Delete(fn)
			heartbeats = append(heartbeats, function.Heartbeat{Function: fn, Timestamp: timestamp})
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
	fn, err := function.FromHeader(req)
	if err != nil {
		if req.Method == http.MethodGet && req.URL.Path == "/healthz" {
			rw.WriteHeader(http.StatusOK)
			return
		}
		log.Error(req.Context(), "failed to get function from header", key.Error.Field(err))
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	go timer.Loop(req.Context(), FlagHeartbeatInterval.Value(), func(ctx context.Context) error {
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
		attempt++

		if attempt > FlagMaxRoundTripAttempts.Value() {
			return nil, fmt.Errorf("failed to proxy request after %d attempts", FlagMaxRoundTripAttempts.Value())
		}

		if attempt > 1 {
			minTimeout := float64(FlagRoundTripRetryMinTimeout.Value())
			maxTimeout := float64(FlagRoundTripRetryMaxTimeout.Value())
			factor := 1 + rand.Float64() // randomize the factor between 1 and 2 to add jitter
			delay := factor * float64(minTimeout) * math.Pow(2, float64(attempt))
			if delay > maxTimeout {
				delay = maxTimeout
			}
			time.Sleep(time.Duration(delay))
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		instance, err := r.controller.Instance(ctx, fn)
		if err != nil {
			log.Warn(ctx, "failed to get instance for function", key.Error.Field(err), key.Function.Field(fn), key.Attempt.Field(attempt))
			continue
		}

		req.URL.Scheme = "http"
		req.URL.Host = instance.Addr

		log.Info(ctx, "forwarding request", key.Instance.Field(instance), key.Attempt.Field(attempt), key.Request.Field(req))
		res, err := r.roundTripper.RoundTrip(req)

		var netOpErr *net.OpError
		if errors.As(err, &netOpErr) {
			if netOpErr.Op == "dial" {
				log.Warn(ctx, "failed to connect to instance", key.Error.Field(err), key.Instance.Field(instance), key.Attempt.Field(attempt))
				continue
			}
		}

		if err != nil {
			log.Error(ctx, "failed to forward request", key.Error.Field(err), key.Instance.Field(instance), key.Attempt.Field(attempt), key.Request.Field(req))
		} else {
			log.Info(ctx, "received response", key.Instance.Field(instance), key.Attempt.Field(attempt), key.Response.Field(res))
		}

		return res, err
	}
}

func rewriteHeaders(pr *httputil.ProxyRequest) {
	function.RemoveHeader(pr.Out)

	pr.Out.Host = pr.In.Host

	var exists bool
	if pr.Out.Header["X-Forwarded-For"], exists = pr.In.Header["X-Forwarded-For"]; !exists {
		if host, _, err := net.SplitHostPort(pr.In.RemoteAddr); err == nil {
			pr.Out.Header["X-Forwarded-For"] = []string{host}
		}
	}

	if pr.Out.Header["X-Forwarded-Host"], exists = pr.In.Header["X-Forwarded-Host"]; !exists {
		pr.Out.Header["X-Forwarded-Host"] = []string{pr.In.Host}
	}

	if pr.Out.Header["X-Forwarded-Proto"], exists = pr.In.Header["X-Forwarded-Proto"]; !exists {
		if pr.In.TLS == nil {
			pr.Out.Header.Set("X-Forwarded-Proto", "http")
		} else {
			pr.Out.Header.Set("X-Forwarded-Proto", "https")
		}
	}

	if pr.Out.Header["Forwarded"], exists = pr.In.Header["Forwarded"]; !exists {
		var forwarded string

		for i, host := range pr.Out.Header["X-Forwarded-For"] {
			if i > 0 {
				forwarded += ", "
			}
			forwarded += "for="
			if ip := net.ParseIP(host); ip == nil || ip.To4() != nil {
				// non-IPv6 addresses can be written as is
				forwarded += host
			} else {
				// IPv6 addresses must be enclosed in square brackets
				// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Forwarded#transitioning_from_x-forwarded-for_to_forwarded
				forwarded += `"[` + host + `]"`
			}
		}

		forwarded += ";host=" + pr.Out.Header["X-Forwarded-Host"][0]
		forwarded += ";proto=" + pr.Out.Header["X-Forwarded-Proto"][0]

		pr.Out.Header["Forwarded"] = []string{forwarded}
	}
}
