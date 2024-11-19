package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

	"github.com/gadget-inc/fusion/internal/controller/hpa"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/hashring"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/pod"
	"github.com/gadget-inc/fusion/internal/timer"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

type Controller struct {
	ip                string
	ring              *hashring.HashRing
	namespaces        []string
	clientset         kubernetes.Interface
	metricsClientset  metricsclientset.Interface
	podManager        *pod.Manager
	deploymentListers sync.Map // map[string]listerv1.DeploymentLister
	controllerProxies sync.Map // map[string]*httputil.ReverseProxy
	assignmentLock    sync.Map // map[string]struct{}
	fnTraffic         map[function.Function]time.Time
	fnTrafficMu       sync.Mutex
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
	err = c.startManagedDeploymentInformer(ctx)
	if err != nil {
		return fmt.Errorf("failed to start managed deployment informer: %w", err)
	}
	err = c.startScalingTenantPods(ctx)
	if err != nil {
		return fmt.Errorf("failed to start scaling tenant pods: %w", err)
	}
	return nil
}

func (c *Controller) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.URL.Path {
	case "/healthz":
		w.WriteHeader(http.StatusOK)
	case "/assign":
		c.handleAssign(w, req)
	case "/traffic":
		c.handleTraffic(w, req)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
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
				slog.DebugContext(ctx, "added controller", key.Pod.Field(pod))
			}
		},
		UpdateFunc: func(_, newObj any) {
			pod := newObj.(*v1.Pod)
			if pod.Status.Phase == v1.PodRunning && pod.Status.PodIP != "" {
				c.ring.Add(pod.Status.PodIP)
				slog.DebugContext(ctx, "updated controller", key.Pod.Field(pod))
			} else {
				c.ring.Remove(pod.Status.PodIP)
				slog.DebugContext(ctx, "removed updated controller", key.Pod.Field(pod))
			}
		},
		DeleteFunc: func(obj any) {
			pod := obj.(*v1.Pod)
			c.ring.Remove(pod.Status.PodIP)
			slog.DebugContext(ctx, "removed deleted controller", key.Pod.Field(pod))
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

func (c *Controller) startManagedDeploymentInformer(ctx context.Context) error {
	slog.InfoContext(ctx, "starting managed deployment informers", slog.Any("namespaces", c.namespaces))

	for _, namespace := range c.namespaces {
		informerFactory := informers.NewSharedInformerFactoryWithOptions(
			c.clientset,
			5*time.Minute,
			informers.WithNamespace(namespace),
			informers.WithTweakListOptions(func(options *metav1.ListOptions) {
				options.LabelSelector = "app.kubernetes.io/managed-by=fusion"
			}),
		)

		deploymentInformer := informerFactory.Apps().V1().Deployments()
		deploymentLister := deploymentInformer.Lister()

		deploymentInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj any) {
				deployment := obj.(*appsv1.Deployment)
				slog.DebugContext(ctx, "deployment added", key.Deployment.Field(deployment))
			},
			UpdateFunc: func(_, newObj any) {
				deployment := newObj.(*appsv1.Deployment)
				slog.DebugContext(ctx, "deployment updated", key.Deployment.Field(deployment))
			},
			DeleteFunc: func(obj any) {
				deployment := obj.(*appsv1.Deployment)
				slog.DebugContext(ctx, "deployment deleted", key.Deployment.Field(deployment))
			},
		})

		_, loaded := c.deploymentListers.LoadOrStore(namespace, deploymentLister)
		if !loaded {
			informerFactory.Start(ctx.Done())
		}

		syncResults := informerFactory.WaitForCacheSync(ctx.Done())
		for informer, synced := range syncResults {
			if !synced {
				return fmt.Errorf("failed to sync managed deployment informer cache: %v", informer)
			}
		}
	}

	return nil
}

func (c *Controller) startScalingTenantPods(ctx context.Context) error {
	// TODO: garbage collect old stabilization windows
	stabilizationWindows := make(map[function.Function]*hpa.StabilizationWindow)

	go timer.Loop(
		ctx,
		15*time.Second,
		func(ctx context.Context) error {
			// scale tenant pods to 0
			c.fnTrafficMu.Lock()
			fnTraffic := c.fnTraffic
			c.fnTraffic = make(map[function.Function]time.Time)
			c.fnTrafficMu.Unlock()

			for _, namespace := range c.namespaces {
				// scale remaining tenant pods
				functionMetrics, err := hpa.GetFunctionMetrics(ctx, c.podManager, c.metricsClientset, namespace)
				if err != nil {
					slog.WarnContext(ctx, "failed to get function metrics", key.Error.Field(err))
					return nil
				}

				now := time.Now()
				for fn, metrics := range functionMetrics {
					lastRequest, ok := fnTraffic[fn]
					if !ok {
						for _, metric := range metrics {
							if metric.AssignedAt.After(lastRequest) {
								lastRequest = metric.AssignedAt
							}
						}
					}

					if time.Since(lastRequest) > 90*time.Second {
						delete(stabilizationWindows, fn)

						controllerIP, ok := c.ring.Get(fn.RingKey())
						if !ok || controllerIP != c.ip {
							slog.DebugContext(ctx, "skipping scaling fn to 0, not assigned to this controller", key.Function.Field(fn), slog.String("controllerIP", controllerIP), slog.String("ip", c.ip), slog.Bool("ok", ok))
							continue
						}

						slog.InfoContext(ctx, "scaling function to 0", key.Function.Field(fn), key.LastRequest.Field(lastRequest))
						err := hpa.ScaleFunction(ctx, c.podManager, fn, 0)
						if err != nil {
							slog.WarnContext(ctx, "failed to scale function", key.Error.Field(err), key.Function.Field(fn))
						}
						continue
					}

					currentReplicas := len(metrics)
					desiredReplicas, err := hpa.CalculateDesiredReplicas(
						currentReplicas,
						metrics,
						int64(fn.TargetCPUUtilization),
						int64(fn.TargetMemoryUtilization),
						hpa.DefaultConfig,
						now,
					)
					if err != nil {
						slog.WarnContext(ctx, "failed to calculate desired replicas", key.Error.Field(err), key.Function.Field(fn))
						continue
					}

					if desiredReplicas < fn.MinReplicas {
						desiredReplicas = fn.MinReplicas
					}

					if desiredReplicas > fn.MaxReplicas {
						desiredReplicas = fn.MaxReplicas
					}

					stabilizationWindow, exists := stabilizationWindows[fn]
					if !exists {
						stabilizationWindow = &hpa.StabilizationWindow{
							Window: hpa.DefaultConfig.DownscaleStabilization,
						}
						stabilizationWindows[fn] = stabilizationWindow
					}

					slog.DebugContext(ctx, "desired replicas",
						key.Function.Field(fn),
						key.CurrentReplicas.Field(currentReplicas),
						key.DesiredReplicas.Field(desiredReplicas),
						slog.Any("maxRecommendation", stabilizationWindow.GetMaxRecommendation()),
					)

					stabilizationWindow.RecordRecommendation(desiredReplicas, now)

					controllerIP, ok := c.ring.Get(fn.RingKey())
					if !ok || controllerIP != c.ip {
						slog.DebugContext(ctx, "skipping scaling for function, not assigned to this controller",
							key.Function.Field(fn),
							slog.String("controllerIP", controllerIP),
							slog.String("ip", c.ip),
							slog.Bool("ok", ok),
						)
						continue
					}

					if desiredReplicas < currentReplicas {
						maxRecommendedReplicas := stabilizationWindow.GetMaxRecommendation()
						if maxRecommendedReplicas < currentReplicas {
							desiredReplicas = maxRecommendedReplicas
						} else {
							desiredReplicas = currentReplicas
						}
					}

					if desiredReplicas == 0 {
						// we only scale to 0 if the last request was more than 90 seconds ago
						slog.DebugContext(ctx, "skipping scaling function to 0 based on hpa", key.Function.Field(fn))
						continue
					}

					slog.DebugContext(ctx, "scaling function",
						key.Function.Field(fn),
						key.CurrentReplicas.Field(currentReplicas),
						key.DesiredReplicas.Field(desiredReplicas),
						slog.Any("maxRecommendation", stabilizationWindow.GetMaxRecommendation()),
					)

					err = hpa.ScaleFunction(ctx, c.podManager, fn, desiredReplicas)
					if err != nil {
						slog.WarnContext(ctx, "failed to scale function",
							key.Error.Field(err),
							key.Function.Field(fn),
							key.CurrentReplicas.Field(currentReplicas),
							key.DesiredReplicas.Field(desiredReplicas),
						)
					}
				}
			}

			return nil
		},
	)

	return nil
}

func (c *Controller) handleAssign(w http.ResponseWriter, req *http.Request) {
	fn, err := function.FromRequest(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	controllerIP, ok := c.ring.Get(fn.RingKey())
	if !ok {
		slog.WarnContext(req.Context(), "no controller for function", key.Function.Field(fn), slog.String("ip", c.ip))
		http.Error(w, "no controller for function", http.StatusServiceUnavailable)
		return
	}

	if controllerIP != c.ip {
		slog.InfoContext(req.Context(), "forwarding request to assigned controller", key.Function.Field(fn), slog.String("ip", controllerIP))
		proxyAny, ok := c.controllerProxies.Load(controllerIP)
		if !ok {
			proxyAny = httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: controllerIP + ":8080"})
			c.controllerProxies.Store(controllerIP, proxyAny)
		}
		proxyAny.(*httputil.ReverseProxy).ServeHTTP(w, req)
		return
	}

	_, assignmentInProgress := c.assignmentLock.LoadOrStore(fn.RingKey(), struct{}{})
	if assignmentInProgress {
		// another goroutine is already assigning a pod for this function
		w.WriteHeader(http.StatusOK)
		return
	}

	defer func() {
		go func() {
			if err == nil {
				// delay releasing the lock so that routers that
				// continue to ask for an assigned pod have time to
				// update their pod informer caches with the new
				// assigned pod
				time.Sleep(3 * time.Second)
			}
			c.assignmentLock.Delete(fn.RingKey())
		}()
	}()

	_, err = timer.Poll(req.Context(), 100*time.Millisecond, 5*time.Second, func(ctx context.Context) (*v1.Pod, error) {
		pod, err := c.podManager.Assign(ctx, fn)
		if err != nil {
			slog.ErrorContext(ctx, "failed to assign pod", key.Error.Field(err), key.Function.Field(fn))
			return nil, nil
		}
		return pod, nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (c *Controller) handleTraffic(w http.ResponseWriter, req *http.Request) {
	var trafficEntries []trafficEntry
	err := json.NewDecoder(req.Body).Decode(&trafficEntries)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	c.fnTrafficMu.Lock()
	defer c.fnTrafficMu.Unlock()
	for _, trafficEntry := range trafficEntries {
		lastRequest, ok := c.fnTraffic[trafficEntry.fn]
		if !ok || trafficEntry.lastRequest.After(lastRequest) {
			c.fnTraffic[trafficEntry.fn] = trafficEntry.lastRequest
		}
	}
}

type trafficEntry struct {
	fn          function.Function
	lastRequest time.Time
}
