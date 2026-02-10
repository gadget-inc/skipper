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
	"strings"
	"time"

	"github.com/gadget-inc/skipper/internal/controller"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/gadget-inc/skipper/internal/telemetry"
	"github.com/gadget-inc/skipper/internal/timer"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/puzpuzpuz/xsync/v4"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	config         *Config
	ctrl           controller.Client
	heartbeats     *xsync.Map[skipper.FunctionHash, *skipper.Heartbeat]
	staleInstances *xsync.Map[string, time.Time] // last ReplaceInstance call time per instance name
	reverseProxy   *httputil.ReverseProxy
	roundTripper   http.RoundTripper
}

func New(cfg *Config, ctrl controller.Client) *Router {
	r := &Router{
		config:         cfg,
		ctrl:           ctrl,
		heartbeats:     xsync.NewMap[skipper.FunctionHash, *skipper.Heartbeat](),
		staleInstances: xsync.NewMap[string, time.Time](),
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
	go timer.Loop(ctx, r.config.HeartbeatInterval, func(ctx context.Context) error {
		var heartbeats []*skipper.Heartbeat
		for fnHash, heartbeat := range r.heartbeats.AllRelaxed() { // duplicate visits are rare and harmless here
			if time.Since(heartbeat.GetTimestamp().AsTime()) > r.config.HeartbeatInterval*3 {
				r.heartbeats.Delete(fnHash) // remove the heartbeat if it hasn't been updated in 3 intervals
			} else {
				heartbeats = append(heartbeats, heartbeat) // otherwise, send the heartbeat
				heartbeatsTotal.WithLabelValues(heartbeat.GetFunction().GetDeployment()).Inc()
			}
		}

		err := r.ctrl.Heartbeat(ctx, r.config.PodIP, heartbeats)
		if err != nil {
			log.Warn(ctx, "failed to send heartbeats", key.Error.Slog(err))
		}

		for name, reportedAt := range r.staleInstances.AllRelaxed() {
			if time.Since(reportedAt) > 30*time.Second {
				r.staleInstances.Delete(name)
			}
		}

		return nil
	})
}

func (r *Router) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	fn, err := skipper.FunctionFromHeader(req)
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
	requestsTotal.WithLabelValues(fn.GetDeployment()).Inc()

	// continuously update the heartbeat timestamp for this function while the request is in flight
	go timer.Loop(ctx, r.config.HeartbeatInterval, func(ctx context.Context) error {
		r.heartbeats.Compute(fn.Hash(), func(heartbeat *skipper.Heartbeat, _ bool) (*skipper.Heartbeat, xsync.ComputeOp) {
			newHeartbeat := &skipper.Heartbeat{}
			newHeartbeat.SetFunction(fn)
			newHeartbeat.SetTimestamp(timestamppb.Now())
			newHeartbeat.SetInFlightRequests(heartbeat.GetInFlightRequests())
			return newHeartbeat, xsync.UpdateOp
		})
		return nil
	})

	// increment the in-flight requests for this function
	r.heartbeats.Compute(fn.Hash(), func(heartbeat *skipper.Heartbeat, _ bool) (*skipper.Heartbeat, xsync.ComputeOp) {
		requestsInFlight.WithLabelValues(fn.GetDeployment()).Inc()
		newHeartbeat := &skipper.Heartbeat{}
		newHeartbeat.SetFunction(fn)
		newHeartbeat.SetTimestamp(timestamppb.Now())
		newHeartbeat.SetInFlightRequests(heartbeat.GetInFlightRequests() + 1)
		return newHeartbeat, xsync.UpdateOp
	})

	// decrement the in-flight requests for this function when the request is complete
	defer r.heartbeats.Compute(fn.Hash(), func(heartbeat *skipper.Heartbeat, _ bool) (*skipper.Heartbeat, xsync.ComputeOp) {
		requestsInFlight.WithLabelValues(fn.GetDeployment()).Dec()
		newHeartbeat := &skipper.Heartbeat{}
		newHeartbeat.SetFunction(fn)
		newHeartbeat.SetTimestamp(timestamppb.Now())
		newHeartbeat.SetInFlightRequests(heartbeat.GetInFlightRequests() - 1)
		return newHeartbeat, xsync.UpdateOp
	})

	r.reverseProxy.ServeHTTP(rw, req.WithContext(withFunction(ctx, fn)))
}

func (r *Router) RoundTrip(req *http.Request) (*http.Response, error) {
	fn, err := functionFromContext(req.Context())
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
		if attempt > r.config.MaxRoundTripAttempts {
			return nil, fmt.Errorf("failed to proxy request after %d attempts", r.config.MaxRoundTripAttempts)
		}

		if attempt > 1 {
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(r.calculateBackoff(attempt)):
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
		req.URL.Host = instance.GetAddr()

		log.Info(ctx, "forwarding request")
		start := time.Now()
		res, err := r.roundTripper.RoundTrip(req)
		duration := time.Since(start)

		ctx = telemetry.With(ctx, key.Response.Attr(res), key.Duration.Attr(duration))

		var netOpErr *net.OpError
		if errors.As(err, &netOpErr) {
			if netOpErr.Op == "dial" {
				log.Warn(ctx, "failed to connect to instance", key.Error.Slog(err))
				excludedInstanceNameSet[instance.GetName()] = struct{}{} // exclude this instance from future requests in case it's the problem
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
			if strings.EqualFold(res.Header.Get("X-Skipper-Instance-Stale"), "true") {
				res.Header.Del("X-Skipper-Instance-Stale")
				lastReported, exists := r.staleInstances.Load(instance.GetName())
				if !exists || time.Since(lastReported) > 10*time.Second {
					r.staleInstances.Store(instance.GetName(), time.Now())
					log.Info(ctx, "detected stale environment, requesting instance replacement", key.InstanceName.Slog(instance.GetName()))
					go func() {
						replaceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
						defer cancel()
						if err := r.ctrl.ReplaceInstance(replaceCtx, fn, instance.GetName()); err != nil {
							log.Warn(replaceCtx, "failed to replace stale instance", key.Error.Slog(err))
						}
					}()
				}
			}
			res.Header[key.GetInstanceDurationMs.Header] = []string{strconv.FormatInt(getInstanceDuration.Milliseconds(), 10)}
		}

		return res, err
	}
}

func rewriteRequestHeaders(pr *httputil.ProxyRequest) {
	delete(pr.Out.Header, key.Function.Header)

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

func (r *Router) calculateBackoff(attempt int) time.Duration {
	minTimeout := float64(r.config.RoundTripRetryMinTimeout)
	maxTimeout := float64(r.config.RoundTripRetryMaxTimeout)
	factor := 1 + rand.Float64() // randomize the factor between 1 and 2 to add jitter
	return time.Duration(min(factor*minTimeout*math.Pow(2, float64(attempt)), maxTimeout))
}
