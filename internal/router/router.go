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
	"sync"
	"time"

	"github.com/gadget-inc/fusion/internal/buffer"
	"github.com/gadget-inc/fusion/internal/controller"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/pod"
	"github.com/gadget-inc/fusion/internal/timer"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

type Router struct {
	controllerClient *controller.Client
	clientset        *kubernetes.Clientset
	podManager       *pod.Manager
	fnProxies        sync.Map
	podStats         sync.Map
}

func New(controllerClient *controller.Client, clientset *kubernetes.Clientset, podManager *pod.Manager) *Router {
	return &Router{controllerClient: controllerClient, clientset: clientset, podManager: podManager}
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

	proxyAny, ok := r.fnProxies.Load(fn)
	if !ok {
		proxyAny = &httputil.ReverseProxy{
			BufferPool: buffer.Pool,
			Director:   func(req *http.Request) {},
			Transport:  r.newRoundTripper(fn),
		}
		r.fnProxies.Store(fn, proxyAny)
	}

	if req.Body != nil {
		req.Body = &nopCloser{req.Body}
		defer req.Body.(*nopCloser).RealClose()
	}

	if req.Header.Get("Connection") == "Upgrade" && req.Header.Get("Upgrade") == "websocket" {
		// TODO: need to associate which pod the websocket is connected to
		slog.InfoContext(req.Context(), "websocket started", key.Function.Field(fn))
		defer func() {
			slog.InfoContext(req.Context(), "websocket ended", key.Function.Field(fn))
		}()
	}

	proxyAny.(*httputil.ReverseProxy).ServeHTTP(rw, req)
}

func (r *Router) newRoundTripper(fn function.Function) http.RoundTripper {
	return &retryingRoundTripper{
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

type retryingRoundTripper struct {
	router    *Router
	fn        function.Function
	transport http.RoundTripper
}

func (rrt *retryingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	attempt := 1
	for {
		if attempt > 3 {
			return nil, fmt.Errorf("failed to get a pod after %d attempts", attempt)
		}

		if attempt > 1 {
			time.Sleep(1 * time.Second * time.Duration(attempt-1))
		}

		pod, err := rrt.getPodFor(ctx, rrt.fn)
		if err != nil {
			slog.WarnContext(ctx, "failed to get a pod for function", key.Error.Field(err), key.Function.Field(rrt.fn))
			attempt++
			continue
		}

		req.URL.Scheme = "http"
		req.URL.Host = pod.Status.PodIP + ":8080"

		res, err := rrt.transport.RoundTrip(req)

		var netOpErr *net.OpError
		if errors.As(err, &netOpErr) {
			if netOpErr.Op == "dial" {
				slog.WarnContext(ctx, "failed to dial pod", key.Error.Field(err), slog.String("pod", pod.Name), key.Function.Field(rrt.fn))
				attempt++
				continue
			}

			if netOpErr.Timeout() {
				slog.WarnContext(ctx, "timeout dialing pod", key.Error.Field(err), slog.String("pod", pod.Name), key.Function.Field(rrt.fn))
				attempt++
				continue
			}
		}

		if err != nil && err != context.Canceled {
			slog.ErrorContext(ctx, "unknown error", key.Error.Field(err), slog.String("pod", pod.Name), key.Function.Field(rrt.fn))
		}

		return res, err
	}
}

func (rrt *retryingRoundTripper) getPodFor(ctx context.Context, fn function.Function) (*v1.Pod, error) {
	return timer.Poll(ctx, 100*time.Millisecond, 5*time.Second, func(ctx context.Context) (*v1.Pod, error) {
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
