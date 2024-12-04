package controller

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/hashring"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	"github.com/gadget-inc/fusion/internal/pod"
	"github.com/gadget-inc/fusion/internal/timer"
	"github.com/puzpuzpuz/xsync/v3"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

const (
	StatusPending    = "pending"
	StatusReady      = "ready"
	StatusUnassigned = "unassigned"
)

var (
	hasTenantRequirement labels.Requirement
	hasTenantSelector    labels.Selector

	doesNotHaveTenantRequirement labels.Requirement
	doesNotHaveTenantSelector    labels.Selector
)

func init() {
	hasTenant, err := labels.NewRequirement(key.Tenant.Label, selection.Exists, nil)
	if err != nil {
		panic(err)
	}

	hasTenantRequirement = *hasTenant
	hasTenantSelector = labels.NewSelector().Add(hasTenantRequirement)

	doesNotHaveTenant, err := labels.NewRequirement(key.Tenant.Label, selection.DoesNotExist, nil)
	if err != nil {
		panic(err)
	}

	doesNotHaveTenantRequirement = *doesNotHaveTenant
	doesNotHaveTenantSelector = labels.NewSelector().Add(doesNotHaveTenantRequirement)
}

type Controller struct {
	ring              *hashring.HashRing
	clientset         kubernetes.Interface
	metricsClientset  metricsclientset.Interface
	podManager        *pod.Manager
	controllerClients *xsync.MapOf[string, *Client]
	scaleMu           *xsync.MapOf[function.Function, sync.Mutex]
	keepAlives        map[function.Function]time.Time
	keepAlivesMu      sync.Mutex // guards keepAlives
}

func New(clientset kubernetes.Interface, metricsClient metricsclientset.Interface, podManager *pod.Manager) *Controller {
	return &Controller{
		ring:              hashring.New(),
		clientset:         clientset,
		metricsClientset:  metricsClient,
		podManager:        podManager,
		controllerClients: xsync.NewMapOf[string, *Client](),
		scaleMu:           xsync.NewMapOf[function.Function, sync.Mutex](),
		keepAlives:        make(map[function.Function]time.Time),
	}
}

func (c *Controller) Start(ctx context.Context) error {
	err := c.startControllerPodInformer(ctx)
	if err != nil {
		return fmt.Errorf("failed to start controller pod informer: %w", err)
	}
	err = c.startManagedReplicaSetInformer(ctx)
	if err != nil {
		return fmt.Errorf("failed to start managed deployment informer: %w", err)
	}
	err = c.startScalingTenantPods(ctx)
	if err != nil {
		return fmt.Errorf("failed to start scaling tenant pods: %w", err)
	}
	return nil
}

func (c *Controller) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	switch req.URL.Path {
	case "/healthz":
		rw.WriteHeader(http.StatusOK)
	case "/get":
		c.handleGet(rw, req)
	case "/scale":
		c.handleScale(rw, req)
	case "/keepalive":
		c.handleKeepAlive(rw, req)
	default:
		http.NotFound(rw, req)
	}
}

func (c *Controller) startControllerPodInformer(ctx context.Context) error {
	controllerPodInformerFactory := informers.NewSharedInformerFactoryWithOptions(
		c.clientset,
		10*time.Minute,
		informers.WithNamespace(FlagNamespace.Value),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = "app.kubernetes.io/name=fusion,app.kubernetes.io/component=controller"
		}),
	)

	controllerPodInformer := controllerPodInformerFactory.Core().V1().Pods().Informer()

	controllerPodHandler, err := controllerPodInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			pod := obj.(*v1.Pod)
			if pod.Status.Phase == v1.PodRunning && pod.Status.PodIP != "" {
				c.ring.Add(pod.Status.PodIP)
				log.Trace(ctx, "added controller", key.Pod.Field(pod))
			}
		},
		UpdateFunc: func(_, newObj any) {
			pod := newObj.(*v1.Pod)
			if pod.Status.Phase == v1.PodRunning && pod.Status.PodIP != "" {
				c.ring.Add(pod.Status.PodIP)
				log.Trace(ctx, "updated controller", key.Pod.Field(pod))
			} else {
				c.ring.Remove(pod.Status.PodIP)
				log.Trace(ctx, "removed updated controller", key.Pod.Field(pod))
			}
		},
		DeleteFunc: func(obj any) {
			pod := obj.(*v1.Pod)
			c.ring.Remove(pod.Status.PodIP)
			log.Trace(ctx, "removed deleted controller", key.Pod.Field(pod))
		},
	})
	if err != nil {
		return fmt.Errorf("failed to add event handler: %w", err)
	}

	controllerPodInformerFactory.Start(ctx.Done())

	synced := cache.WaitForCacheSync(ctx.Done(), controllerPodHandler.HasSynced)
	if !synced {
		return fmt.Errorf("failed to sync controller pod informer")
	}

	go func() {
		timer.Loop(ctx, 10*time.Second, func(ctx context.Context) error {
			controllerIPs := c.ring.List()
			log.Trace(ctx, "controller pods", slog.Any("ips", controllerIPs))
			return nil
		})
	}()

	return nil
}
