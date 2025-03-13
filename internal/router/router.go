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

	"github.com/gadget-inc/skipper/internal/controller"
	"github.com/gadget-inc/skipper/internal/function"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/telemetry"
	"github.com/gadget-inc/skipper/internal/timer"
	"github.com/puzpuzpuz/xsync/v3"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type Router struct {
	ctrl         controller.Client
	heartbeats   *xsync.MapOf[function.Function, function.Heartbeat]
	reverseProxy *httputil.ReverseProxy
	roundTripper http.RoundTripper
}

func New(ctrl controller.Client) *Router {
	r := &Router{
		ctrl:       ctrl,
		heartbeats: xsync.NewMapOf[function.Function, function.Heartbeat](),
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
		ErrorLog:   log.StdLogger(),
	}

	return r
}

func (r *Router) Start(ctx context.Context) {
	go timer.Loop(ctx, FlagHeartbeatInterval.Value(), func(ctx context.Context) error {
		var heartbeats []function.Heartbeat
		r.heartbeats.Range(func(fn function.Function, heartbeat function.Heartbeat) bool {
			if time.Since(heartbeat.Timestamp) > FlagHeartbeatInterval.Value()*3 {
				r.heartbeats.Delete(fn) // remove the heartbeat if it hasn't been updated in 3 intervals
			} else {
				heartbeats = append(heartbeats, heartbeat) // otherwise, send the heartbeat
			}
			return true
		})

		err := r.ctrl.Heartbeat(ctx, FlagPodIP.Value(), heartbeats)
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

	ctx := function.With(req.Context(), fn)
	ctx = log.With(ctx, key.Function.Field(fn))
	ctx = telemetry.WithPropagatedAttributes(ctx, key.Function.Attributes(fn)...)

	// continuously update the heartbeat timestamp for this function while the request is in flight
	go timer.Loop(ctx, FlagHeartbeatInterval.Value(), func(ctx context.Context) error {
		r.heartbeats.Compute(fn, func(heartbeat function.Heartbeat, _ bool) (function.Heartbeat, bool) {
			heartbeat.Function = fn
			heartbeat.Timestamp = time.Now()
			return heartbeat, false
		})
		return nil
	})

	// increment the in-flight requests for this function
	r.heartbeats.Compute(fn, func(heartbeat function.Heartbeat, _ bool) (function.Heartbeat, bool) {
		heartbeat.Function = fn
		heartbeat.Timestamp = time.Now()
		heartbeat.InFlightRequests++
		return heartbeat, false
	})

	// decrement the in-flight requests for this function when the request is complete
	defer r.heartbeats.Compute(fn, func(heartbeat function.Heartbeat, _ bool) (function.Heartbeat, bool) {
		heartbeat.Function = fn
		heartbeat.Timestamp = time.Now()
		heartbeat.InFlightRequests--
		return heartbeat, false
	})

	r.reverseProxy.ServeHTTP(rw, req.WithContext(ctx))
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
			delay := min(factor*minTimeout*math.Pow(2, float64(attempt)), maxTimeout)
			time.Sleep(time.Duration(delay))
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		instance, err := r.ctrl.Instance(ctx, fn)
		if err != nil {
			log.Warn(ctx, "failed to get instance for function", key.Error.Field(err), key.Attempt.Field(attempt))
			continue
		}

		req.URL.Scheme = "http"
		req.URL.Host = instance.Addr

		log.Info(ctx, "forwarding request", key.Instance.Field(instance), key.Attempt.Field(attempt), key.Request.Field(req))
		start := time.Now()
		res, err := r.roundTripper.RoundTrip(req)
		duration := time.Since(start)

		var netOpErr *net.OpError
		if errors.As(err, &netOpErr) {
			if netOpErr.Op == "dial" {
				log.Warn(ctx, "failed to connect to instance", key.Error.Field(err), key.Instance.Field(instance), key.Attempt.Field(attempt), key.Duration.Field(duration))
				continue
			}
		}

		if err != nil {
			log.Error(ctx, "failed to forward request", key.Error.Field(err), key.Instance.Field(instance), key.Attempt.Field(attempt), key.Request.Field(req), key.Duration.Field(duration))
		} else {
			log.Info(ctx, "forwarding response", key.Instance.Field(instance), key.Attempt.Field(attempt), key.Response.Field(res), key.Duration.Field(duration))
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
