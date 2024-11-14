package router

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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type Router struct {
	ip             string
	ring           *hashring.HashRing
	clientset      *kubernetes.Clientset
	podManager     *pod.Manager
	assignmentLock sync.Map
	routerProxies  sync.Map
}

func New(ip string, clientset *kubernetes.Clientset, podManager *pod.Manager) *Router {
	return &Router{ip: ip, ring: hashring.New(), clientset: clientset, podManager: podManager}
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	dest, err := destination.New(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	routerIP, ok := r.ring.Get(dest.String())
	if !ok {
		slog.WarnContext(req.Context(), "no router for destination", slog.String("destination", dest.String()), slog.String("ip", r.ip))
		http.Error(w, "no router for destination", http.StatusServiceUnavailable)
		return
	}

	if routerIP != r.ip {
		slog.InfoContext(req.Context(), "forwarding request to assigned router", slog.String("destination", dest.String()), slog.String("ip", routerIP))
		proxyAny, ok := r.routerProxies.Load(routerIP)
		if !ok {
			proxyAny = httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: routerIP + ":8080"})
			r.routerProxies.Store(routerIP, proxyAny)
		}
		proxyAny.(*httputil.ReverseProxy).ServeHTTP(w, req)
		return
	}

	ctx := req.Context()
	pod, err := r.podManager.GetOrAssignFor(ctx, dest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	pod.ServeHTTP(w, req)
}

func (r *Router) Start(ctx context.Context, fusionNamespace string) error {
	slog.InfoContext(ctx, "starting router informer", slog.String("namespace", fusionNamespace))

	informerFactory := informers.NewSharedInformerFactoryWithOptions(
		r.clientset,
		5*time.Minute,
		informers.WithNamespace(fusionNamespace),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = labels.SelectorFromSet(labels.Set{
				"app.kubernetes.io/name":      "fusion",
				"app.kubernetes.io/component": "router",
			}).String()
		}),
	)

	podInformer := informerFactory.Core().V1().Pods().Informer()

	_, err := podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			pod := obj.(*v1.Pod)
			if pod.Status.Phase == v1.PodRunning && pod.Status.PodIP != "" {
				r.ring.Add(pod.Status.PodIP)
				slog.DebugContext(ctx, "added router", slog.String("name", pod.Name), slog.String("ip", pod.Status.PodIP))
			}
		},
		UpdateFunc: func(_, newObj any) {
			pod := newObj.(*v1.Pod)
			if pod.Status.Phase == v1.PodRunning && pod.Status.PodIP != "" {
				r.ring.Add(pod.Status.PodIP)
				slog.DebugContext(ctx, "updated router", slog.String("name", pod.Name), slog.String("ip", pod.Status.PodIP))
			} else {
				r.ring.Remove(pod.Status.PodIP)
				slog.DebugContext(ctx, "removed updated router", slog.String("name", pod.Name), slog.String("ip", pod.Status.PodIP), slog.String("phase", string(pod.Status.Phase)))
			}
		},
		DeleteFunc: func(obj any) {
			pod := obj.(*v1.Pod)
			r.ring.Remove(pod.Status.PodIP)
			slog.DebugContext(ctx, "removed deleted router", slog.String("name", pod.Name), slog.String("ip", pod.Status.PodIP))
		},
	})
	if err != nil {
		return fmt.Errorf("failed to add event handler: %w", err)
	}

	informerFactory.Start(ctx.Done())

	syncResults := informerFactory.WaitForCacheSync(ctx.Done())
	for informer, synced := range syncResults {
		if !synced {
			return fmt.Errorf("failed to sync router informer cache: %v", informer)
		}
	}

	go func() {
		timer.Loop(ctx, 1*time.Second, func(ctx context.Context) error {
			nodes := r.ring.List()
			slog.InfoContext(ctx, "routers", slog.Any("ips", nodes))
			return nil
		})
	}()

	return nil
}
