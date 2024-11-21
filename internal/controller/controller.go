package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/gadget-inc/fusion/internal/buffer"
	"github.com/gadget-inc/fusion/internal/controller/hpa"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/hashring"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	"github.com/gadget-inc/fusion/internal/pod"
	"github.com/gadget-inc/fusion/internal/timer"
	"github.com/puzpuzpuz/xsync/v3"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
	"k8s.io/utils/strings/slices"
)

type Controller struct {
	ring              *hashring.HashRing
	clientset         kubernetes.Interface
	metricsClientset  metricsclientset.Interface
	podManager        *pod.Manager
	controllerProxies *xsync.MapOf[string, *httputil.ReverseProxy]
	assignmentLock    *xsync.MapOf[string, struct{}]
	fnTraffic         map[function.Function]time.Time
	fnTrafficMu       sync.Mutex // guards fnTraffic
}

func New(clientset kubernetes.Interface, metricsClient metricsclientset.Interface, podManager *pod.Manager) *Controller {
	return &Controller{
		ring:              hashring.New(),
		clientset:         clientset,
		metricsClientset:  metricsClient,
		podManager:        podManager,
		controllerProxies: xsync.NewMapOf[string, *httputil.ReverseProxy](),
		assignmentLock:    xsync.NewMapOf[string, struct{}](),
		fnTraffic:         make(map[function.Function]time.Time),
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
	case "/assign":
		c.handleAssign(rw, req)
	case "/traffic":
		c.handleTraffic(rw, req)
	default:
		http.Error(rw, "not found", http.StatusNotFound)
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

func (c *Controller) startManagedReplicaSetInformer(ctx context.Context) error {
	log.Info(ctx, "starting managed replica set informers", slog.Any("namespaces", function.FlagNamespaces.Value))

	for _, namespace := range function.FlagNamespaces.Value {
		informerFactory := informers.NewSharedInformerFactoryWithOptions(
			c.clientset,
			5*time.Minute,
			informers.WithNamespace(namespace),
			informers.WithTweakListOptions(func(options *metav1.ListOptions) {
				options.LabelSelector = "app.kubernetes.io/managed-by=fusion"
			}),
		)

		replicaSetInformer := informerFactory.Apps().V1().ReplicaSets()
		replicaSetLister := replicaSetInformer.Lister()

		_, err := replicaSetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj any) {
				replicaSet := obj.(*appsv1.ReplicaSet)
				log.Trace(ctx, "replica set added", key.ReplicaSet.Field(replicaSet))
			},
			UpdateFunc: func(_, newObj any) {
				replicaSet := newObj.(*appsv1.ReplicaSet)
				log.Trace(ctx, "replica set updated", key.ReplicaSet.Field(replicaSet))
			},
			DeleteFunc: func(obj any) {
				replicaSet := obj.(*appsv1.ReplicaSet)
				log.Trace(ctx, "replica set deleted", key.ReplicaSet.Field(replicaSet))
			},
		})
		if err != nil {
			return fmt.Errorf("failed to add replica set event handler: %w", err)
		}

		informerFactory.Start(ctx.Done())

		syncResults := informerFactory.WaitForCacheSync(ctx.Done())
		for informer, synced := range syncResults {
			if !synced {
				return fmt.Errorf("failed to sync managed deployment informer cache: %v", informer)
			}
		}

		go timer.Loop(ctx, 10*time.Second, func(ctx context.Context) error {
			pods, err := c.podManager.GetAllAssignedPods(namespace)
			if err != nil {
				log.Warn(ctx, "failed to get all assigned pods for replica set check", key.Error.Field(err))
				return nil
			}

			var defunctFunctions []function.Instance
			for _, pod := range pods {
				fn, err := function.FromPod(pod)
				if err != nil {
					log.Warn(ctx, "failed to get function from pod", key.Error.Field(err), key.Pod.Field(pod))
					continue
				}

				replicaSet, err := replicaSetLister.ReplicaSets(pod.Namespace).Get(fn.ReplicaSet)
				if err != nil {
					log.Warn(ctx, "failed to get replica set for pod", key.Pod.Field(pod), key.Error.Field(err))
					continue
				}

				if replicaSet.Spec.Replicas == nil || *replicaSet.Spec.Replicas == 0 {
					defunctFunctions = append(defunctFunctions, fn)
				}
			}

			for _, fn := range defunctFunctions {
				log.Debug(ctx, "terminating defunct function", key.Pod.Field(fn.Pod), key.Function.Field(fn))
				err = c.podManager.Terminate(ctx, fn.Function, fn.Pod)
				if err != nil {
					log.Warn(ctx, "failed to terminate pod", key.Error.Field(err), key.Pod.Field(fn.Pod), key.Function.Field(fn))
				}
				time.Sleep(1 * time.Second)
			}

			return nil
		})
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
			c.fnTrafficMu.Lock()
			fnTraffic := maps.Clone(c.fnTraffic)
			c.fnTrafficMu.Unlock()

			for _, namespace := range function.FlagNamespaces.Value {
				functionMetrics, err := hpa.GetFunctionMetrics(ctx, c.podManager, c.metricsClientset, namespace)
				if err != nil {
					log.Warn(ctx, "failed to get function metrics", key.Error.Field(err))
					return nil
				}

				now := time.Now()
				for fn, metrics := range functionMetrics {
					lastRequest, ok := fnTraffic[fn]
					if !ok {
						log.Warn(ctx, "no traffic entry for function", key.Function.Field(fn))
						for _, metric := range metrics {
							if metric.AssignedAt.After(lastRequest) {
								lastRequest = metric.AssignedAt
							}
						}
					}

					if time.Since(lastRequest) > 90*time.Second {
						delete(stabilizationWindows, fn)

						controllerIP, ok := c.ring.Get(fn.RingKey())
						if !ok || controllerIP != FlagIP.Value {
							log.Trace(
								ctx,
								"skipping scaling fn to 0, not assigned to this controller",
								key.Function.Field(fn),
								slog.String("controllerIP", controllerIP),
								slog.String("ip", FlagIP.Value),
								slog.Bool("ok", ok),
							)
							continue
						}

						log.Info(ctx, "scaling function to 0", key.Function.Field(fn), key.LastRequest.Field(lastRequest))
						err := hpa.ScaleFunction(ctx, c.podManager, fn, 0)
						if err != nil {
							log.Warn(ctx, "failed to scale function", key.Error.Field(err), key.Function.Field(fn))
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
						log.Warn(ctx, "failed to calculate desired replicas", key.Error.Field(err), key.Function.Field(fn))
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

					log.Trace(ctx, "desired replicas",
						key.Function.Field(fn),
						key.CurrentReplicas.Field(currentReplicas),
						key.DesiredReplicas.Field(desiredReplicas),
						slog.Any("maxRecommendation", stabilizationWindow.GetMaxRecommendation()),
					)

					stabilizationWindow.RecordRecommendation(desiredReplicas, now)

					controllerIP, ok := c.ring.Get(fn.RingKey())
					if !ok || controllerIP != FlagIP.Value {
						log.Trace(ctx, "skipping scaling for function, not assigned to this controller",
							key.Function.Field(fn),
							slog.String("controllerIP", controllerIP),
							slog.String("ip", FlagIP.Value),
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
						log.Debug(ctx, "skipping scaling function to 0 based on hpa", key.Function.Field(fn))
						continue
					}

					log.Trace(ctx, "scaling function",
						key.Function.Field(fn),
						key.CurrentReplicas.Field(currentReplicas),
						key.DesiredReplicas.Field(desiredReplicas),
						slog.Any("maxRecommendation", stabilizationWindow.GetMaxRecommendation()),
					)

					err = hpa.ScaleFunction(ctx, c.podManager, fn, desiredReplicas)
					if err != nil {
						log.Warn(ctx, "failed to scale function",
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

func (c *Controller) handleAssign(rw http.ResponseWriter, req *http.Request) {
	fn, err := function.FromRequest(req)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	controllerIP, ok := c.ring.Get(fn.RingKey())
	if !ok {
		log.Warn(req.Context(), "no controller for function", key.Function.Field(fn), slog.String("ip", FlagIP.Value))
		http.Error(rw, "no controller for function", http.StatusServiceUnavailable)
		return
	}

	if controllerIP != FlagIP.Value {
		log.Info(req.Context(), "forwarding request to assigned controller", key.Function.Field(fn), slog.String("ip", controllerIP))
		controllerProxy, ok := c.controllerProxies.Load(controllerIP)
		if !ok {
			controllerProxy = httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: controllerIP + ":" + strconv.Itoa(FlagPort.Value)})
			controllerProxy.BufferPool = buffer.Pool
			c.controllerProxies.Store(controllerIP, controllerProxy)
		}
		controllerProxy.ServeHTTP(rw, req)
		return
	}

	_, assignmentInProgress := c.assignmentLock.LoadOrStore(fn.RingKey(), struct{}{})
	if assignmentInProgress {
		// another goroutine is already assigning a pod for this function
		rw.WriteHeader(http.StatusOK)
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

	_, err = timer.PollUntil(req.Context(), 250*time.Millisecond, func(ctx context.Context) (*v1.Pod, error) {
		pod, err := c.podManager.Assign(ctx, fn)
		if err != nil {
			log.Error(ctx, "failed to assign pod", key.Error.Field(err), key.Function.Field(fn))
			return nil, nil
		}
		return pod, nil
	})
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}

	rw.WriteHeader(http.StatusOK)
}

type TrafficEntry struct {
	Function    function.Function `json:"function"`
	LastRequest time.Time         `json:"lastRequest"`
}

func (c *Controller) handleTraffic(rw http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	var trafficEntries []TrafficEntry
	err = json.Unmarshal(body, &trafficEntries)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	c.fnTrafficMu.Lock()
	for _, trafficEntry := range trafficEntries {
		trafficEntry.Function.Metadata = "" // the idle function reaper doesn't have the function metadata, so we need to clear it to match the function in the map
		lastRequest, ok := c.fnTraffic[trafficEntry.Function]
		if !ok || trafficEntry.LastRequest.After(lastRequest) {
			c.fnTraffic[trafficEntry.Function] = trafficEntry.LastRequest
		}
	}
	defer c.fnTrafficMu.Unlock()

	log.Trace(req.Context(), "received traffic", slog.Int("trafficEntries", len(trafficEntries)))
	rw.WriteHeader(http.StatusOK)

	go func() {
		forwardedFor := req.Header.Values(key.ForwardedFor.Header)
		forwardedFor = append(forwardedFor, FlagIP.Value)

		for _, controllerIP := range c.ring.List() {
			if !slices.Contains(forwardedFor, controllerIP) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				controllerPort := strconv.Itoa(FlagPort.Value)
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+controllerIP+":"+controllerPort+"/traffic", bytes.NewBuffer(body))
				if err != nil {
					log.Warn(ctx, "failed to create traffic request", key.Error.Field(err))
					continue
				}

				req.Header.Set("Content-Type", "application/json")
				for _, forwardedForIP := range forwardedFor {
					req.Header.Add(key.ForwardedFor.Header, forwardedForIP)
				}

				log.Trace(ctx, "forwarding traffic", slog.String("controllerIP", controllerIP), key.ForwardedFor.Field(forwardedFor))
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					log.Warn(ctx, "failed to forward traffic", key.Error.Field(err))
					continue
				}

				if res.StatusCode != http.StatusOK {
					log.Warn(ctx, "forwarded traffic failed", slog.String("status", res.Status))
				}
			}
		}
	}()
}
