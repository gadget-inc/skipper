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
	}, []string{"function_deployment", "function_tenant"})

	requestsInFlight = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "skipper",
		Subsystem: "router",
		Name:      "requests_in_flight",
		Help:      "The number of requests that are currently being handled by the router",
	}, []string{"function_deployment", "function_tenant"})

	heartbeatsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "skipper",
		Subsystem: "router",
		Name:      "heartbeats_total",
		Help:      "The number of heartbeats sent by the router",
	}, []string{"function_deployment", "function_tenant"})
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
				heartbeatsTotal.WithLabelValues(fn.Deployment, fn.Tenant).Inc()
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
	requestsTotal.WithLabelValues(fn.Deployment, fn.Tenant).Inc()

	// continuously update the heartbeat timestamp for this function while the request is in flight
	go timer.Loop(ctx, FlagHeartbeatInterval.Value(), func(ctx context.Context) error {
		r.heartbeats.Compute(fn, func(heartbeat function.Heartbeat, _ bool) (function.Heartbeat, xsync.ComputeOp) {
			heartbeat.Function = fn
			heartbeat.Timestamp = time.Now()
			return heartbeat, xsync.UpdateOp
		})
		return nil
	})

	r.reverseProxy.ServeHTTP(rw, req.WithContext(function.With(ctx, fn)))
}

func (r *Router) RoundTrip(req *http.Request) (*http.Response, error) {
	fn, err := function.From(req.Context())
	if err != nil {
		return nil, err
	}

	getInstanceDuration := time.Duration(0)

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

		getInstanceStart := time.Now()
		instance, err := r.ctrl.Instance(ctx, fn)
		if err != nil {
			log.Warn(ctx, "failed to get instance for function", key.Error.Field(err))
			continue
		}

		getInstanceDuration += time.Since(getInstanceStart)

		ctx = log.With(ctx, key.Instance.Field(instance))
		ctx = telemetry.WithPropagatedAttributes(ctx, key.Instance.Attributes(instance)...)

		forwardReq := req.WithContext(ctx)
		forwardReq.URL.Scheme = "http"
		forwardReq.URL.Host = instance.Addr

		log.Info(ctx, "forwarding request")
		start := time.Now()
		res, err := r.forward(forwardReq, fn, instance)
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

		if res != nil {
			telemetry.SetAttributes(ctx, key.GetInstanceDurationMs.Attribute(getInstanceDuration))
			res.Header[key.GetInstanceDurationMs.Header] = []string{strconv.FormatInt(getInstanceDuration.Milliseconds(), 10)}
		}

		return res, err
	}
}

func (r *Router) forward(req *http.Request, fn function.Function, instance *function.Instance) (*http.Response, error) {
	r.incrementInFlight(fn, instance)
	defer r.decrementInFlight(fn, instance)
	return r.roundTripper.RoundTrip(req)
}

func (r *Router) incrementInFlight(fn function.Function, instance *function.Instance) {
	requestsInFlight.WithLabelValues(fn.Deployment, fn.Tenant).Inc()

	r.heartbeats.Compute(fn, func(heartbeat function.Heartbeat, _ bool) (function.Heartbeat, xsync.ComputeOp) {
		heartbeat.Function = fn
		heartbeat.Timestamp = time.Now()
		heartbeat.InFlightRequests++

		if heartbeat.InFlightPerInstance == nil {
			heartbeat.InFlightPerInstance = make(map[string]int)
		}
		heartbeat.InFlightPerInstance[instance.Name]++

		return heartbeat, xsync.UpdateOp
	})
}

func (r *Router) decrementInFlight(fn function.Function, instance *function.Instance) {
	requestsInFlight.WithLabelValues(fn.Deployment, fn.Tenant).Dec()

	r.heartbeats.Compute(fn, func(heartbeat function.Heartbeat, _ bool) (function.Heartbeat, xsync.ComputeOp) {
		heartbeat.Function = fn
		heartbeat.Timestamp = time.Now()
		if heartbeat.InFlightRequests > 0 {
			heartbeat.InFlightRequests--
		}

		if heartbeat.InFlightPerInstance != nil && instance != nil {
			if count, ok := heartbeat.InFlightPerInstance[instance.Name]; ok {
				if count <= 1 {
					delete(heartbeat.InFlightPerInstance, instance.Name)
				} else {
					heartbeat.InFlightPerInstance[instance.Name] = count - 1
				}
			}
		}

		return heartbeat, xsync.UpdateOp
	})
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
