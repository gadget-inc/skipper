package controller

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

	"github.com/gadget-inc/fusion/internal/destination"
	"github.com/gadget-inc/fusion/internal/hashring"
	"github.com/gadget-inc/fusion/internal/pod"
	"github.com/gadget-inc/fusion/internal/timer"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type Controller struct {
	ip                string
	ring              *hashring.HashRing
	clientset         *kubernetes.Clientset
	podManager        *pod.Manager
	controllerProxies sync.Map
	assignmentLock    sync.Map
}

func New(ip string, clientset *kubernetes.Clientset, podManager *pod.Manager) *Controller {
	return &Controller{ip: ip, ring: hashring.New(), clientset: clientset, podManager: podManager}
}

func (c *Controller) Start(ctx context.Context, fusionNamespace string) error {
	slog.InfoContext(ctx, "starting controller informer", slog.String("namespace", fusionNamespace))

	informerFactory := informers.NewSharedInformerFactoryWithOptions(
		c.clientset,
		5*time.Minute,
		informers.WithNamespace(fusionNamespace),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = "app.kubernetes.io/name=fusion,app.kubernetes.io/component=controller"
		}),
	)

	podInformer := informerFactory.Core().V1().Pods().Informer()

	_, err := podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			pod := obj.(*v1.Pod)
			if pod.Status.Phase == v1.PodRunning && pod.Status.PodIP != "" {
				c.ring.Add(pod.Status.PodIP)
				slog.DebugContext(ctx, "added controller", slog.String("name", pod.Name), slog.String("ip", pod.Status.PodIP))
			}
		},
		UpdateFunc: func(_, newObj any) {
			pod := newObj.(*v1.Pod)
			if pod.Status.Phase == v1.PodRunning && pod.Status.PodIP != "" {
				c.ring.Add(pod.Status.PodIP)
				slog.DebugContext(ctx, "updated controller", slog.String("name", pod.Name), slog.String("ip", pod.Status.PodIP))
			} else {
				c.ring.Remove(pod.Status.PodIP)
				slog.DebugContext(ctx, "removed updated controller", slog.String("name", pod.Name), slog.String("ip", pod.Status.PodIP), slog.String("phase", string(pod.Status.Phase)))
			}
		},
		DeleteFunc: func(obj any) {
			pod := obj.(*v1.Pod)
			c.ring.Remove(pod.Status.PodIP)
			slog.DebugContext(ctx, "removed deleted controller", slog.String("name", pod.Name), slog.String("ip", pod.Status.PodIP))
		},
	})
	if err != nil {
		return fmt.Errorf("failed to add event handler: %w", err)
	}

	informerFactory.Start(ctx.Done())

	syncResults := informerFactory.WaitForCacheSync(ctx.Done())
	for informer, synced := range syncResults {
		if !synced {
			return fmt.Errorf("failed to sync controller informer cache: %v", informer)
		}
	}

	go func() {
		timer.Loop(ctx, 10*time.Second, func(ctx context.Context) error {
			nodes := c.ring.List()
			slog.InfoContext(ctx, "controllers", slog.Any("ips", nodes))
			return nil
		})
	}()

	return nil
}

func (c *Controller) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	dest, err := destination.New(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	controllerIP, ok := c.ring.Get(dest.String())
	if !ok {
		slog.WarnContext(req.Context(), "no controller for destination", slog.String("destination", dest.String()), slog.String("ip", c.ip))
		http.Error(w, "no controller for destination", http.StatusServiceUnavailable)
		return
	}

	if controllerIP != c.ip {
		slog.InfoContext(req.Context(), "forwarding request to assigned controller", slog.String("destination", dest.String()), slog.String("ip", controllerIP))
		proxyAny, ok := c.controllerProxies.Load(controllerIP)
		if !ok {
			proxyAny = httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: controllerIP + ":8080"})
			c.controllerProxies.Store(controllerIP, proxyAny)
		}
		proxyAny.(*httputil.ReverseProxy).ServeHTTP(w, req)
		return
	}

	_, assignmentInProgress := c.assignmentLock.LoadOrStore(dest.String(), struct{}{})
	if assignmentInProgress {
		// another goroutine is already assigning a pod for this destination
		w.WriteHeader(http.StatusOK)
		return
	}
	defer c.assignmentLock.Delete(dest.String())

	_, err = timer.Poll(req.Context(), 100*time.Millisecond, 5*time.Second, func(ctx context.Context) (*pod.Pod, error) {
		availablePods, err := c.podManager.GetAvailable(dest)
		if err != nil {
			return nil, fmt.Errorf("failed to list available pods: %w", err)
		}
		if len(availablePods) == 0 {
			slog.WarnContext(ctx, "no available pods", slog.Any("destination", dest))
			return nil, nil
		}

		for _, pod := range availablePods {
			err := c.podManager.Assign(ctx, pod, dest)
			if err != nil {
				slog.ErrorContext(ctx, "failed to assign pod", slog.Any("error", err), slog.Any("destination", dest))
				continue
			}
			return pod, nil
		}

		return nil, nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
