package router

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"
	"time"

	"github.com/gadget-inc/fusion/internal/buffer"
	"github.com/gadget-inc/fusion/internal/controller"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	"github.com/gadget-inc/fusion/internal/pod"
	"github.com/gadget-inc/fusion/internal/timer"
	"github.com/puzpuzpuz/xsync/v3"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

type Router struct {
	controllerClient *controller.Client
	podManager       *pod.Manager
	fnProxy          *httputil.ReverseProxy
	fnTraffic        *xsync.MapOf[function.Function, time.Time]
	transport        *http.Transport
}

func New(controllerClient *controller.Client, clientset *kubernetes.Clientset, podManager *pod.Manager) *Router {
	r := &Router{
		controllerClient: controllerClient,
		podManager:       podManager,
		fnTraffic:        xsync.NewMapOf[function.Function, time.Time](),
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

	r.fnProxy = &httputil.ReverseProxy{
		BufferPool: buffer.Pool,
		Rewrite:    rewriteHeaders,
		Transport:  r,
	}

	return r
}

func (r *Router) Start(ctx context.Context) {
	go timer.Loop(ctx, 3*time.Second, func(ctx context.Context) error {
		fnTraffic := r.fnTraffic
		r.fnTraffic = xsync.NewMapOf[function.Function, time.Time]()

		var trafficEntries []controller.TrafficEntry
		fnTraffic.Range(func(fn function.Function, lastRequest time.Time) bool {
			trafficEntries = append(trafficEntries, controller.TrafficEntry{Function: fn, LastRequest: lastRequest})
			return true
		})

		log.Trace(ctx, "sending traffic", slog.Int("entries", len(trafficEntries)))
		err := r.controllerClient.Traffic(ctx, trafficEntries)
		if err != nil {
			log.Warn(ctx, "failed to send traffic", key.Error.Field(err))
		}
		return nil
	})
}

func (r *Router) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	fn, err := function.FromHeaders(req)
	if err != nil {
		if req.URL.Path == "/healthz" {
			rw.WriteHeader(http.StatusOK)
			return
		}

		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	r.fnTraffic.Store(fn, time.Now())
	go func() {
		for {
			select {
			case <-req.Context().Done():
				err := req.Context().Err()
				if errors.Is(err, context.Canceled) {
					err = nil
				}
				log.Debug(req.Context(), "request done", key.Function.Field(fn), slog.String("url", req.URL.String()), key.Error.Field(err))
				return
			case <-time.After(5 * time.Second):
				log.Debug(req.Context(), "request active", key.Function.Field(fn), slog.String("url", req.URL.String()))
				r.fnTraffic.Store(fn, time.Now())
			}
		}
	}()

	r.fnProxy.ServeHTTP(rw, req.WithContext(function.With(req.Context(), fn)))
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

		if attempt > 2 {
			return nil, fmt.Errorf("failed to get a pod after %d attempts", attempt)
		}

		if attempt > 0 {
			time.Sleep(1 * time.Second * time.Duration(attempt))
		}

		pod, err := r.getAssignedPod(ctx, fn)
		if err != nil {
			log.Warn(ctx, "failed to get assigned pod for function", key.Error.Field(err), key.Function.Field(fn))
			attempt++
			continue
		}

		req.URL.Scheme = "http"
		req.URL.Host = pod.Status.PodIP + ":" + strconv.Itoa(function.FlagPort.Value)

		res, err := r.transport.RoundTrip(req)

		var netOpErr *net.OpError
		if errors.As(err, &netOpErr) {
			if netOpErr.Op == "dial" {
				log.Warn(ctx, "failed to dial pod", key.Error.Field(err), key.Pod.Field(pod), key.Function.Field(fn))
				attempt++
				continue
			}

			if netOpErr.Timeout() {
				log.Warn(ctx, "timeout dialing pod", key.Error.Field(err), key.Pod.Field(pod), key.Function.Field(fn))
				attempt++
				continue
			}
		}

		if err != nil && err != context.Canceled {
			log.Error(ctx, "unknown error", key.Error.Field(err), key.Pod.Field(pod), key.Function.Field(fn))
		}

		return res, err
	}
}

func (r *Router) getAssignedPod(ctx context.Context, fn function.Function) (*v1.Pod, error) {
	return timer.PollUntil(ctx, 250*time.Millisecond, func(ctx context.Context) (*v1.Pod, error) {
		pods, err := r.podManager.GetAssigned(fn)
		if err != nil {
			return nil, fmt.Errorf("failed to list assigned pods: %w", err)
		}
		if len(pods) > 0 {
			return pods[rand.Intn(len(pods))], nil
		}
		return nil, r.controllerClient.Assign(ctx, fn)
	})
}

func rewriteHeaders(r *httputil.ProxyRequest) {
	function.RemoveHeaders(r.Out)
	r.Out.Host = r.In.Host
	r.Out.Header["X-Forwarded-For"] = r.In.Header["X-Forwarded-For"]
	r.SetXForwarded()
}
