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
	"github.com/gadget-inc/fusion/internal/destination"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/pod"
	"github.com/gadget-inc/fusion/internal/timer"
	"k8s.io/client-go/kubernetes"
)

type Router struct {
	controllerClient *controller.Client
	clientset        *kubernetes.Clientset
	podManager       *pod.Manager
}

func New(controllerClient *controller.Client, clientset *kubernetes.Clientset, podManager *pod.Manager) *Router {
	return &Router{controllerClient: controllerClient, clientset: clientset, podManager: podManager}
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	dest, err := destination.New(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	proxy := &httputil.ReverseProxy{
		BufferPool: buffer.Pool,
		Director:   func(req *http.Request) {},
		Transport:  r.newRoundTripper(dest),
	}

	if req.Body != nil {
		req.Body = &nopCloser{req.Body}
		defer req.Body.(*nopCloser).RealClose()
	}

	proxy.ServeHTTP(w, req)
}

func (r *Router) newRoundTripper(dest destination.Destination) http.RoundTripper {
	return &retryingRoundTripper{
		router: r,
		dest:   dest,
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
	dest      destination.Destination
	transport http.RoundTripper
}

func (rrt *retryingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	attempt := 1
	for {
		if attempt > 1 {
			time.Sleep(1 * time.Second * time.Duration(attempt-1))
		}

		if attempt > 3 {
			return nil, fmt.Errorf("failed to get a pod after %d attempts", attempt)
		}

		pod, err := rrt.getPodFor(ctx, rrt.dest)
		if err != nil {
			slog.WarnContext(ctx, "failed to get a pod for destination", slog.Any("error", err), key.Destination.Field(rrt.dest))
			attempt++
			continue
		}

		req.URL.Scheme = "http"
		req.URL.Host = pod.Status.PodIP + ":8080"

		res, err := rrt.transport.RoundTrip(req)

		var netOpErr *net.OpError
		if errors.As(err, &netOpErr) {
			if netOpErr.Op == "dial" {
				slog.WarnContext(ctx, "failed to dial pod", slog.Any("error", err), slog.String("pod", pod.Name), key.Destination.Field(rrt.dest))
				attempt++
				continue
			}

			if netOpErr.Timeout() {
				slog.WarnContext(ctx, "timeout dialing pod", slog.Any("error", err), slog.String("pod", pod.Name), key.Destination.Field(rrt.dest))
				attempt++
				continue
			}
		}

		if err != nil && err != context.Canceled {
			slog.ErrorContext(ctx, "unknown error", slog.Any("error", err), slog.String("pod", pod.Name), key.Destination.Field(rrt.dest))
		}

		return res, err
	}
}

func (rrt *retryingRoundTripper) getPodFor(ctx context.Context, dest destination.Destination) (*pod.Pod, error) {
	return timer.Poll(ctx, 100*time.Millisecond, 5*time.Second, func(ctx context.Context) (*pod.Pod, error) {
		pods, err := rrt.router.podManager.GetAssigned(dest)
		if err != nil {
			return nil, fmt.Errorf("failed to list assigned pods: %w", err)
		}
		if len(pods) > 0 {
			return pod.New(pods[rand.Intn(len(pods))]), nil
		}
		return nil, rrt.router.controllerClient.Assign(ctx, dest)
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
