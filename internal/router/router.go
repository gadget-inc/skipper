package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
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
	clientset        *kubernetes.Clientset
	podManager       *pod.Manager
	fnProxies        *xsync.MapOf[function.Function, *httputil.ReverseProxy]
	fnTraffic        *xsync.MapOf[function.Function, time.Time]
}

func New(controllerClient *controller.Client, clientset *kubernetes.Clientset, podManager *pod.Manager) *Router {
	return &Router{
		controllerClient: controllerClient,
		clientset:        clientset,
		podManager:       podManager,
		fnProxies:        xsync.NewMapOf[function.Function, *httputil.ReverseProxy](),
		fnTraffic:        xsync.NewMapOf[function.Function, time.Time](),
	}
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
	fn, err := function.FromRequest(req)
	if err != nil {
		if req.URL.Path == "/healthz" {
			rw.WriteHeader(http.StatusOK)
			return
		}

		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	r.fnTraffic.Store(fn, time.Now())

	fnProxy, ok := r.fnProxies.Load(fn)
	if !ok {
		fnProxy = &httputil.ReverseProxy{
			BufferPool: buffer.Pool,
			Director:   func(req *http.Request) {},
			Transport:  r.newRoundTripper(fn),
		}
		r.fnProxies.Store(fn, fnProxy)
	}

	if req.Body != nil {
		req.Body = &nopCloser{req.Body}
		defer req.Body.(*nopCloser).RealClose()
	}

	// if req.Header.Get("Connection") == "Upgrade" && req.Header.Get("Upgrade") == "websocket" {
	// 	log.Debug(req.Context(), "websocket started", key.Function.Field(fn))
	// 	go timer.Loop(req.Context(), 3*time.Second, func(ctx context.Context) error {
	// 		r.fnTraffic.Store(fn, time.Now())
	// 		return nil
	// 	})
	// 	defer log.Debug(req.Context(), "websocket ended", key.Function.Field(fn))
	// }

	go func() {
		for {
			select {
			case <-req.Context().Done():
				log.Debug(req.Context(), "request done", key.Function.Field(fn), slog.String("url", req.URL.String()), key.Error.Field(req.Context().Err()))
				return
			case <-time.After(5 * time.Second):
				log.Debug(req.Context(), "request active", key.Function.Field(fn), slog.String("url", req.URL.String()))
			}
		}
	}()

	fnProxy.ServeHTTP(rw, req)
}

func (r *Router) newRoundTripper(fn function.Function) http.RoundTripper {
	return &fnRoundTripper{
		router: r,
		fn:     fn,
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
}

type fnRoundTripper struct {
	router    *Router
	fn        function.Function
	transport http.RoundTripper
}

func (frt *fnRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	attempt := 0
	for {
		if attempt > 2 {
			return nil, fmt.Errorf("failed to get a pod after %d attempts", attempt)
		}

		if attempt > 0 {
			time.Sleep(1 * time.Second * time.Duration(attempt))
		}

		pod, err := frt.getAssignedPod(ctx, frt.fn)
		if err != nil {
			log.Warn(ctx, "failed to get a pod for function", key.Error.Field(err), key.Function.Field(frt.fn))
			attempt++
			continue
		}

		req.URL.Scheme = "http"
		req.URL.Host = pod.Status.PodIP + ":8888"

		res, err := frt.transport.RoundTrip(req)

		var netOpErr *net.OpError
		if errors.As(err, &netOpErr) {
			if netOpErr.Op == "dial" {
				log.Warn(ctx, "failed to dial pod", key.Error.Field(err), slog.String("pod", pod.Name), key.Function.Field(frt.fn))
				attempt++
				continue
			}

			if netOpErr.Timeout() {
				log.Warn(ctx, "timeout dialing pod", key.Error.Field(err), slog.String("pod", pod.Name), key.Function.Field(frt.fn))
				attempt++
				continue
			}
		}

		if err != nil && err != context.Canceled {
			log.Error(ctx, "unknown error", key.Error.Field(err), slog.String("pod", pod.Name), key.Function.Field(frt.fn))
		}

		return res, err
	}
}

func (rrt *fnRoundTripper) getAssignedPod(ctx context.Context, fn function.Function) (*v1.Pod, error) {
	return timer.PollUntil(ctx, 250*time.Millisecond, func(ctx context.Context) (*v1.Pod, error) {
		pods, err := rrt.router.podManager.GetAssigned(fn)
		if err != nil {
			return nil, fmt.Errorf("failed to list assigned pods: %w", err)
		}
		if len(pods) > 0 {
			return pods[rand.Intn(len(pods))], nil
		}
		return nil, rrt.router.controllerClient.Assign(ctx, fn)
	})
}

type nopCloser struct{ io.ReadCloser }

func (n *nopCloser) Close() error { return nil }

func (n *nopCloser) RealClose() error {
	if n.ReadCloser == nil {
		return nil
	}
	return n.ReadCloser.Close()
}
