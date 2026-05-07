package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httputil"
	"slices"
	"strconv"
	"time"

	"github.com/gadget-inc/skipper/internal/controller"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/metric"
	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/gadget-inc/skipper/internal/telemetry"
	"github.com/gadget-inc/skipper/internal/timer"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/puzpuzpuz/xsync/v4"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// metrics holds every prometheus metric the router package
// registers, captured for the docs `{{< metricsTable router >}}`
// shortcode. Help strings are doc-quality (single sentence, optional
// trailing parenthetical, <= 120 chars).
var metrics = metric.NewSet(prometheus.DefaultRegisterer)

var (
	requestsTotal = metrics.NewCounterVec(prometheus.CounterOpts{
		Namespace: "skipper",
		Subsystem: "router",
		Name:      "requests_total",
		Help:      "Total requests processed by the router.",
	}, []string{"function_deployment", "assignment_deployment"})

	requestsInFlight = metrics.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "skipper",
		Subsystem: "router",
		Name:      "requests_in_flight",
		Help:      "Requests currently being handled by the router.",
	}, []string{"function_deployment", "assignment_deployment"})

	heartbeatsTotal = metrics.NewCounterVec(prometheus.CounterOpts{
		Namespace: "skipper",
		Subsystem: "router",
		Name:      "heartbeats_total",
		Help:      "Heartbeats sent to the controller.",
	}, []string{"function_deployment", "assignment_deployment"})
)

// Metrics returns the metadata of every prometheus metric registered
// by the router package. The docs site reads this slice via the
// `{{< metricsTable router >}}` shortcode.
func Metrics() []metric.Metric {
	return metrics.Slice()
}

type Router struct {
	config       *Config
	ctrl         controller.Client
	heartbeats   *xsync.Map[skipper.AssignmentHash, *heartbeatState]
	reverseProxy *httputil.ReverseProxy
	roundTripper http.RoundTripper
}

func New(cfg *Config, ctrl controller.Client) *Router {
	r := &Router{
		config:       cfg,
		ctrl:         ctrl,
		heartbeats:   xsync.NewMap[skipper.AssignmentHash, *heartbeatState](),
		roundTripper: otelhttp.NewTransport(newHTTPTransport(DefaultHTTPTransportSettings)),
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
		for fnHash, state := range r.heartbeats.AllRelaxed() { // duplicate visits are rare and harmless here
			if time.Since(state.lastActiveTime()) > r.config.HeartbeatInterval*3 {
				r.heartbeats.Delete(fnHash) // remove the heartbeat if it hasn't been updated in 3 intervals
			} else {
				hb := state.toProto() // materialise proto only when sending
				heartbeats = append(heartbeats, hb)
				deployment := hb.GetAssignment().GetDeployment()
				heartbeatsTotal.WithLabelValues(deployment, deployment).Inc()
			}
		}

		err := r.ctrl.Heartbeat(ctx, r.config.PodIP, heartbeats)
		if err != nil {
			log.Warn(ctx, "failed to send heartbeats", key.Error.Slog(err))
		}
		return nil
	})
}

func (r *Router) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	fn, err := skipper.AssignmentFromHeader(req)
	if err != nil {
		if req.Method == http.MethodGet && req.URL.Path == "/healthz" {
			rw.WriteHeader(http.StatusOK)
			return
		}
		log.Error(req.Context(), "failed to get function from header", key.Error.Slog(err))
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := telemetry.With(req.Context(), key.Request.Attr(req), key.URL.Attr(req.URL), skipper.LegacyFunctionKey.Attr(fn), skipper.AssignmentKey.Attr(fn))
	deployment := fn.GetDeployment()
	requestsTotal.WithLabelValues(deployment, deployment).Inc()

	// get or create the heartbeat state for this function
	state, loaded := r.heartbeats.LoadOrCompute(fn.Hash(), func() (*heartbeatState, bool) {
		return newHeartbeatState(fn), false
	})
	if loaded {
		state.updateAssignment(fn)
	}

	// continuously update the heartbeat timestamp for this function while the request is in flight
	go timer.Loop(ctx, r.config.HeartbeatInterval, func(ctx context.Context) error {
		state.touch()
		return nil
	})

	// increment the in-flight requests for this function
	requestsInFlight.WithLabelValues(deployment, deployment).Inc()
	state.inFlight.Add(1)
	state.touch()

	// decrement the in-flight requests for this function when the request is complete
	defer func() {
		requestsInFlight.WithLabelValues(deployment, deployment).Dec()
		state.inFlight.Add(-1)
		state.touch()
	}()

	reqCtx := withAssignment(ctx, fn)

	// For oneshot functions, track the assigned instance so we can release it after the request.
	var instResult *instanceResult
	if fn.GetOneshot() {
		instResult = &instanceResult{}
		reqCtx = withInstanceResult(reqCtx, instResult)
	}

	// Go 1.26's ReverseProxy wraps the body with a noopCloseReader that
	// doesn't propagate Close to the underlying body. We close it here to
	// ensure cleanup. Safe to double-close if future Go versions revert.
	if req.Body != nil && req.Body != http.NoBody {
		defer req.Body.Close()
	}

	r.reverseProxy.ServeHTTP(rw, req.WithContext(reqCtx))

	// After the response is sent for a oneshot function, release the instance.
	if instResult != nil && instResult.instance != nil {
		r.releaseInstance(instResult.instance)
	}
}

func (r *Router) RoundTrip(req *http.Request) (*http.Response, error) {
	fn, err := assignmentFromContext(req.Context())
	if err != nil {
		return nil, err
	}

	if req.Body != nil && req.Body != http.NoBody {
		// Wrap the request body to prevent the underlying http.RoundTripper
		// from closing it on dial errors, allowing reuse between retries.
		//
		// This is especially important with Go 1.26+: ReverseProxy wraps
		// the body with a noopCloseReader whose Close sets an atomic flag
		// that makes subsequent Read calls return an error. Without this
		// NopCloser layer, a transport Close on dial failure would poison
		// the noopCloseReader and break retry attempts.
		originalBody := req.Body
		req.Body = io.NopCloser(originalBody)
		defer func() { req.Body = originalBody }()
	}

	var excludedInstanceNames []string
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

		ctx := telemetry.With(req.Context(), key.Attempt.Attr(attempt), key.ExcludeInstanceNames.Attr(excludedInstanceNames))

		getInstanceStart := time.Now()
		instance, err := r.ctrl.Instance(ctx, fn, excludedInstanceNames...)
		getInstanceDuration += time.Since(getInstanceStart)
		if err != nil {
			log.Warn(ctx, "failed to get instance for function", key.Error.Slog(err))
			continue
		}

		ctx = telemetry.With(ctx, skipper.InstanceKey.Attr(instance))

		// Store the instance for oneshot release after the request completes.
		if ir := instanceResultFromContext(ctx); ir != nil {
			ir.instance = instance
		}

		req := req.WithContext(ctx)
		req.URL.Scheme = "http"
		req.URL.Host = instance.GetAddr()

		log.Debug(ctx, "forwarding request")
		start := time.Now()
		res, err := r.roundTripper.RoundTrip(req)
		duration := time.Since(start)

		ctx = telemetry.With(ctx, key.Response.Attr(res), key.Duration.Attr(duration))

		var netOpErr *net.OpError
		if errors.As(err, &netOpErr) {
			if netOpErr.Op == "dial" {
				log.Warn(ctx, "failed to connect to instance", key.Error.Slog(err))
				if !slices.Contains(excludedInstanceNames, instance.GetName()) {
					excludedInstanceNames = append(excludedInstanceNames, instance.GetName()) // exclude this instance from future attempts
				}
				// For oneshot functions, release the failed instance immediately
				// to prevent pod leaks when retrying.
				if ir := instanceResultFromContext(ctx); ir != nil {
					ir.instance = nil
					r.releaseInstance(instance)
				}
				continue
			}
		}

		ctx = telemetry.With(ctx, key.GetInstanceDurationMs.Attr(getInstanceDuration))

		if err != nil {
			log.Error(ctx, "failed to forward request", key.Error.Slog(err))
		} else {
			log.Debug(ctx, "forwarding response")
		}

		if res != nil {
			res.Header[key.GetInstanceDurationMs.Header] = []string{strconv.FormatInt(getInstanceDuration.Milliseconds(), 10)}
		}

		return res, err
	}
}

func rewriteRequestHeaders(pr *httputil.ProxyRequest) {
	delete(pr.Out.Header, skipper.LegacyFunctionKey.Header)
	delete(pr.Out.Header, skipper.AssignmentKey.Header)

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
		buf := getForwardedBuf()

		for i, host := range pr.Out.Header["X-Forwarded-For"] {
			if i > 0 {
				buf.WriteString(", ")
			}
			buf.WriteString("for=")
			if ip := net.ParseIP(host); ip == nil || ip.To4() != nil {
				// non-IPv6 addresses can be written as is
				buf.WriteString(host)
			} else {
				// IPv6 addresses must be enclosed in square brackets
				// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Forwarded#transitioning_from_x-forwarded-for_to_forwarded
				buf.WriteString(`"[`)
				buf.WriteString(host)
				buf.WriteString(`]"`)
			}
		}

		buf.WriteString(";host=")
		buf.WriteString(pr.Out.Header["X-Forwarded-Host"][0])
		buf.WriteString(";proto=")
		buf.WriteString(pr.Out.Header["X-Forwarded-Proto"][0])

		pr.Out.Header["Forwarded"] = []string{buf.String()}
		putForwardedBuf(buf)
	}
}

// releaseInstance deletes a oneshot pod in a fire-and-forget goroutine.
// It uses a background context because the request context may already be done.
func (r *Router) releaseInstance(inst *skipper.Instance) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := r.ctrl.ReleaseInstance(ctx, inst); err != nil {
			log.Warn(ctx, "failed to release oneshot instance", key.Error.Slog(err), skipper.InstanceKey.Slog(inst))
		}
	}()
}

func (r *Router) calculateBackoff(attempt int) time.Duration {
	minTimeout := float64(r.config.RoundTripRetryMinTimeout)
	maxTimeout := float64(r.config.RoundTripRetryMaxTimeout)
	factor := 1 + rand.Float64() // randomize the factor between 1 and 2 to add jitter
	return time.Duration(min(factor*minTimeout*math.Pow(2, float64(attempt)), maxTimeout))
}
