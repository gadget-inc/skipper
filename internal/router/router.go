package router

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	"go.opentelemetry.io/otel/metric"
)

var (
	requestsCounter = unwrap(telemetry.Meter.Int64Counter("skipper.router.requests",
		metric.WithDescription("The number of requests handled by the router"),
		metric.WithUnit("{request}"),
	))

	inFlightRequestsCounter = unwrap(telemetry.Meter.Int64UpDownCounter("skipper.router.in_flight_requests",
		metric.WithDescription("The number of requests that are currently being handled by the router"),
		metric.WithUnit("{request}"),
	))

	heartbeatsCounter = unwrap(telemetry.Meter.Int64Counter("skipper.router.heartbeats",
		metric.WithDescription("The number of heartbeats sent by the router"),
		metric.WithUnit("{heartbeat}"),
	))
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
		Transport:  r,
		Rewrite:    rewriteHeaders,
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
				heartbeatsCounter.Add(ctx, 1, metric.WithAttributeSet(key.Function.AttributesSet(fn)))
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

	ctx := log.With(req.Context(), key.Function.Field(fn))
	ctx = telemetry.WithPropagatedAttributes(ctx, key.Function.Attributes(fn)...)
	requestsCounter.Add(ctx, 1, metric.WithAttributeSet(key.Function.AttributesSet(fn)))

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
		inFlightRequestsCounter.Add(ctx, 1, metric.WithAttributeSet(key.Function.AttributesSet(fn)))
		return heartbeat, false
	})

	// decrement the in-flight requests for this function when the request is complete
	defer r.heartbeats.Compute(fn, func(heartbeat function.Heartbeat, _ bool) (function.Heartbeat, bool) {
		heartbeat.Function = fn
		heartbeat.Timestamp = time.Now()
		heartbeat.InFlightRequests--
		inFlightRequestsCounter.Add(ctx, -1, metric.WithAttributeSet(key.Function.AttributesSet(fn)))
		return heartbeat, false
	})

	r.reverseProxy.ServeHTTP(rw, req.WithContext(function.With(ctx, fn)))
}

func (r *Router) RoundTrip(req *http.Request) (*http.Response, error) {
	fn, err := function.From(req.Context())
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
		case <-req.Context().Done():
			return nil, req.Context().Err()
		default:
		}

		ctx := log.With(req.Context(), key.Attempt.Field(attempt))
		ctx = telemetry.WithPropagatedAttributes(ctx, key.Attempt.Attribute(attempt))

		instance, err := r.ctrl.Instance(ctx, fn)
		if err != nil {
			log.Warn(ctx, "failed to get instance for function", key.Error.Field(err))
			continue
		}

		ctx = log.With(ctx, key.Instance.Field(instance))
		ctx = telemetry.WithPropagatedAttributes(ctx, key.Instance.Attributes(instance)...)

		req := req.WithContext(ctx)
		req.URL.Scheme = "http"
		req.URL.Host = instance.Addr

		log.Info(ctx, "forwarding request")
		start := time.Now()
		res, err := r.roundTripper.RoundTrip(req)
		duration := time.Since(start)

		ctx = log.With(ctx, key.Response.Field(res), key.Duration.Field(duration))
		ctx = telemetry.WithPropagatedAttributes(ctx, append(key.Response.Attributes(res), key.Duration.Attribute(duration))...)

		var netOpErr *net.OpError
		if errors.As(err, &netOpErr) {
			if netOpErr.Op == "dial" {
				log.Warn(ctx, "failed to connect to instance", key.Error.Field(err))
				continue
			}
		}

		if err != nil {
			log.Error(ctx, "failed to forward request", key.Error.Field(err))
		} else {
			log.Info(ctx, "forwarding response")
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
