package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httputil"
	"slices"
	"strconv"
	"time"

	"github.com/gadget-inc/skipper/internal/controller"
	"github.com/gadget-inc/skipper/internal/function"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/telemetry"
	"github.com/gadget-inc/skipper/internal/timer"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/puzpuzpuz/xsync/v4"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

var (
	requestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "skipper",
		Subsystem: "router",
		Name:      "requests_total",
		Help:      "The number of requests handled by the router",
	}, []string{"function_deployment"})

	requestsInFlight = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "skipper",
		Subsystem: "router",
		Name:      "requests_in_flight",
		Help:      "The number of requests that are currently being handled by the router",
	}, []string{"function_deployment"})

	heartbeatsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "skipper",
		Subsystem: "router",
		Name:      "heartbeats_total",
		Help:      "The number of heartbeats sent by the router",
	}, []string{"function_deployment"})
)

type Router struct {
	ctrl         controller.Client
	heartbeats   *xsync.Map[function.Function, function.Heartbeat]
	reverseProxy *httputil.ReverseProxy
	roundTripper http.RoundTripper
}

func New(ctrl controller.Client) *Router {
	r := &Router{
		ctrl:       ctrl,
		heartbeats: xsync.NewMap[function.Function, function.Heartbeat](),
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
		Transport:  r,
		Rewrite:    rewriteRequestHeaders,
		ErrorLog:   log.StdLogger(slog.LevelError),
		BufferPool: bufferPool,
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
				heartbeatsTotal.WithLabelValues(fn.Deployment).Inc()
			}
			return true
		})

		err := r.ctrl.Heartbeat(ctx, FlagPodIP.Value(), heartbeats)
		if err != nil {
			log.Warn(ctx, "failed to send heartbeats", key.Error.Slog(err))
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
		log.Error(req.Context(), "failed to get function from header", key.Error.Slog(err))
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := telemetry.With(req.Context(), key.Request.Attr(req), key.URL.Attr(req.URL), key.Function.Attr(fn))
	requestsTotal.WithLabelValues(fn.Deployment).Inc()

	// continuously update the heartbeat timestamp for this function while the request is in flight
	go timer.Loop(ctx, FlagHeartbeatInterval.Value(), func(ctx context.Context) error {
		r.heartbeats.Compute(fn, func(heartbeat function.Heartbeat, _ bool) (function.Heartbeat, xsync.ComputeOp) {
			heartbeat.Function = fn
			heartbeat.Timestamp = time.Now()
			return heartbeat, xsync.UpdateOp
		})
		return nil
	})

	// increment the in-flight requests for this function
	r.heartbeats.Compute(fn, func(heartbeat function.Heartbeat, _ bool) (function.Heartbeat, xsync.ComputeOp) {
		heartbeat.Function = fn
		heartbeat.Timestamp = time.Now()
		heartbeat.InFlightRequests++
		requestsInFlight.WithLabelValues(fn.Deployment).Inc()
		return heartbeat, xsync.UpdateOp
	})

	// decrement the in-flight requests for this function when the request is complete
	defer r.heartbeats.Compute(fn, func(heartbeat function.Heartbeat, _ bool) (function.Heartbeat, xsync.ComputeOp) {
		heartbeat.Function = fn
		heartbeat.Timestamp = time.Now()
		heartbeat.InFlightRequests--
		requestsInFlight.WithLabelValues(fn.Deployment).Dec()
		return heartbeat, xsync.UpdateOp
	})

	r.reverseProxy.ServeHTTP(rw, req.WithContext(function.With(ctx, fn)))
}

func (r *Router) RoundTrip(req *http.Request) (*http.Response, error) {
	fn, err := function.From(req.Context())
	if err != nil {
		return nil, err
	}

	if req.Body != nil && req.Body != http.NoBody {
		// wrap the request body with io.NopCloser to prevent the
		// underlying http.RoundTripper from closing it on dial errors,
		// allowing the body to be reused between retry attempts
		originalBody := req.Body
		req.Body = io.NopCloser(originalBody)
		defer func() { req.Body = originalBody }()
	}

	excludedInstanceNameSet := make(map[string]struct{})
	getInstanceDuration := time.Duration(0)
	attempt := 0

	for {
		attempt++
		if attempt > FlagMaxRoundTripAttempts.Value() {
			return nil, fmt.Errorf("failed to proxy request after %d attempts", FlagMaxRoundTripAttempts.Value())
		}

		if attempt > 1 {
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(calculateBackoff(attempt)):
			}
		}

		excludedInstanceNames := slices.Collect(maps.Keys(excludedInstanceNameSet))
		ctx := telemetry.With(req.Context(), key.Attempt.Attr(attempt), key.ExcludeInstanceNames.Attr(excludedInstanceNames))

		getInstanceStart := time.Now()
		instance, err := r.ctrl.Instance(ctx, fn, excludedInstanceNames...)
		getInstanceDuration += time.Since(getInstanceStart)
		if err != nil {
			log.Warn(ctx, "failed to get instance for function", key.Error.Slog(err))
			continue
		}

		ctx = telemetry.With(ctx, key.Instance.Attr(instance))

		req := req.WithContext(ctx)
		req.URL.Scheme = "http"
		req.URL.Host = instance.Addr

		log.Info(ctx, "forwarding request")
		start := time.Now()
		res, err := r.roundTripper.RoundTrip(req)
		duration := time.Since(start)

		ctx = telemetry.With(ctx, key.Response.Attr(res), key.Duration.Attr(duration))

		var netOpErr *net.OpError
		if errors.As(err, &netOpErr) {
			if netOpErr.Op == "dial" {
				log.Warn(ctx, "failed to connect to instance", key.Error.Slog(err))
				excludedInstanceNameSet[instance.Name] = struct{}{} // exclude this instance from future requests in case it's the problem
				continue
			}
		}

		ctx = telemetry.With(ctx, key.GetInstanceDurationMs.Attr(getInstanceDuration))

		if err != nil {
			log.Error(ctx, "failed to forward request", key.Error.Slog(err))
		} else {
			log.Info(ctx, "forwarding response")
		}

		if res != nil {
			res.Header[key.GetInstanceDurationMs.Header] = []string{strconv.FormatInt(getInstanceDuration.Milliseconds(), 10)}
		}

		return res, err
	}
}

func rewriteRequestHeaders(pr *httputil.ProxyRequest) {
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

func calculateBackoff(attempt int) time.Duration {
	minTimeout := float64(FlagRoundTripRetryMinTimeout.Value())
	maxTimeout := float64(FlagRoundTripRetryMaxTimeout.Value())
	factor := 1 + rand.Float64() // randomize the factor between 1 and 2 to add jitter
	return time.Duration(min(factor*minTimeout*math.Pow(2, float64(attempt)), maxTimeout))
}
