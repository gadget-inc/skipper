package controller

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gadget-inc/fusion/internal/destination"
	"github.com/gadget-inc/fusion/internal/hashring"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/pod"
	"github.com/gadget-inc/fusion/internal/timer"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

type Controller struct {
	ip                string
	ring              *hashring.HashRing
	namespaces        []string
	clientset         kubernetes.Interface
	metricsClientset  metricsclientset.Interface
	podManager        *pod.Manager
	controllerProxies sync.Map
	assignmentLock    sync.Map
}

func New(ip string, namespaces []string, clientset kubernetes.Interface, metricsClient metricsclientset.Interface, podManager *pod.Manager) *Controller {
	return &Controller{
		ip:               ip,
		ring:             hashring.New(),
		namespaces:       namespaces,
		clientset:        clientset,
		metricsClientset: metricsClient,
		podManager:       podManager,
	}
}

func (c *Controller) Start(ctx context.Context, controllerNamespace string) error {
	err := c.startControllerPodInformer(ctx, controllerNamespace)
	if err != nil {
		return fmt.Errorf("failed to start controller pod informer: %w", err)
	}
	err = c.scaleTenantPods(ctx)
	if err != nil {
		return fmt.Errorf("failed to start tenant pod informer: %w", err)
	}
	return nil
}

func (c *Controller) startControllerPodInformer(ctx context.Context, controllerNamespace string) error {
	controllerPodInformerFactory := informers.NewSharedInformerFactoryWithOptions(
		c.clientset,
		10*time.Minute,
		informers.WithNamespace(controllerNamespace),
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

	controllerPodInformerFactory.Start(ctx.Done())

	synced := cache.WaitForCacheSync(ctx.Done(), controllerPodHandler.HasSynced)
	if !synced {
		return fmt.Errorf("failed to sync controller pod informer")
	}

	go func() {
		timer.Loop(ctx, 10*time.Second, func(ctx context.Context) error {
			nodes := c.ring.List()
			slog.InfoContext(ctx, "controller pods", slog.Any("ips", nodes))
			return nil
		})
	}()

	return nil
}

func (c *Controller) scaleTenantPods(ctx context.Context) error {
	for _, namespace := range c.namespaces {
		go func() {
			timer.Loop(ctx, 15*time.Second, func(ctx context.Context) error {
				podMetricList, err := c.metricsClientset.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/managed-by=fusion," + key.Tenant.Label,
				})
				if err != nil {
					slog.WarnContext(ctx, "failed to list pod metrics", slog.Any("error", err))
				}

				podMetricsByTenant := make(map[string][]*v1beta1.PodMetrics)
				for _, podMetric := range podMetricList.Items {
					tenantDeployment := podMetric.Labels[key.Deployment.Label] + "/" + podMetric.Labels[key.Tenant.Label]
					podMetricsByTenant[tenantDeployment] = append(podMetricsByTenant[tenantDeployment], &podMetric)
				}

				for tenantDeployment, podMetrics := range podMetricsByTenant {
					var totalCPUUsageMilli int64
					var totalMemoryUsageBytes int64

					for _, podMetric := range podMetrics {
						for _, containerMetric := range podMetric.Containers {
							totalCPUUsageMilli += containerMetric.Usage.Cpu().MilliValue()
							totalMemoryUsageBytes += containerMetric.Usage.Memory().Value()
						}
					}

					averageCPUUsageMilli := totalCPUUsageMilli / int64(len(podMetrics))
					averageMemoryUsageBytes := totalMemoryUsageBytes / int64(len(podMetrics))

					split := strings.Split(tenantDeployment, "/")
					slog.InfoContext(ctx, "pod metrics",
						slog.String("deployment", split[0]), slog.String("tenant", split[1]),
						slog.Int64("total_cpu", totalCPUUsageMilli), slog.Int64("total_memory", totalMemoryUsageBytes),
						slog.Int64("average_cpu", averageCPUUsageMilli), slog.Int64("average_memory", averageMemoryUsageBytes))
				}

				return nil
			})
		}()
	}
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
		slog.WarnContext(req.Context(), "no controller for destination", key.Destination.Field(dest), slog.String("ip", c.ip))
		http.Error(w, "no controller for destination", http.StatusServiceUnavailable)
		return
	}

	if controllerIP != c.ip {
		slog.InfoContext(req.Context(), "forwarding request to assigned controller", key.Destination.Field(dest), slog.String("ip", controllerIP))
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

	defer func() {
		go func() {
			if err == nil {
				// delay releasing the lock to give informers time to update their caches with the new assignment
				time.Sleep(10 * time.Second)
			}
			c.assignmentLock.Delete(dest.String())
		}()
	}()

	_, err = timer.Poll(req.Context(), 100*time.Millisecond, 5*time.Second, func(ctx context.Context) (*pod.Pod, error) {
		availablePods, err := c.podManager.GetAvailable(dest)
		if err != nil {
			return nil, fmt.Errorf("failed to list available pods: %w", err)
		}
		if len(availablePods) == 0 {
			slog.WarnContext(ctx, "no available pods", key.Destination.Field(dest))
			return nil, nil
		}

		pod := availablePods[rand.Intn(len(availablePods))]
		err = c.podManager.Assign(ctx, pod, dest)
		if err != nil {
			slog.ErrorContext(ctx, "failed to assign pod", slog.Any("error", err), key.Destination.Field(dest))
			return nil, nil
		}
		return pod, nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
