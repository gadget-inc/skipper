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
	"slices"
	"strconv"
	"sync"
	"time"

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
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

type Controller struct {
	ring              *hashring.HashRing
	clientset         kubernetes.Interface
	metricsClientset  metricsclientset.Interface
	podManager        *pod.Manager
	controllerClients *xsync.MapOf[string, *Client]
	assignmentLock    *xsync.MapOf[function.Function, struct{}]
	fnTraffic         map[function.Function]time.Time
	fnTrafficMu       sync.Mutex // guards fnTraffic
}

func New(clientset kubernetes.Interface, metricsClient metricsclientset.Interface, podManager *pod.Manager) *Controller {
	return &Controller{
		ring:              hashring.New(),
		clientset:         clientset,
		metricsClientset:  metricsClient,
		podManager:        podManager,
		controllerClients: xsync.NewMapOf[string, *Client](),
		assignmentLock:    xsync.NewMapOf[function.Function, struct{}](),
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
				options.LabelSelector = key.Deployment.Label
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
	stabilizationWindows := make(map[function.Function]*StabilizationWindow)

	go timer.Loop(
		ctx,
		15*time.Second,
		func(ctx context.Context) error {
			c.fnTrafficMu.Lock()
			fnTraffic := maps.Clone(c.fnTraffic)
			c.fnTrafficMu.Unlock()

			for _, namespace := range function.FlagNamespaces.Value {
				functionMetrics, err := c.getFunctionMetrics(ctx, namespace)
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

						log.Trace(ctx, "scaling function to 0", key.Function.Field(fn), key.LastRequest.Field(lastRequest))
						err := c.scaleFunction(ctx, fn, 0)
						if err != nil {
							log.Warn(ctx, "failed to scale function", key.Error.Field(err), key.Function.Field(fn))
						}
						continue
					}

					currentReplicas := len(metrics)
					desiredReplicas, err := calculateDesiredReplicas(
						currentReplicas,
						metrics,
						int64(fn.TargetCPUUtilization),
						int64(fn.TargetMemoryUtilization),
						DefaultConfig,
						now,
					)
					if err != nil {
						log.Trace(ctx, "failed to calculate desired replicas", key.Error.Field(err), key.Function.Field(fn))
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
						stabilizationWindow = &StabilizationWindow{
							Window: DefaultConfig.DownscaleStabilization,
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

					err = c.scaleFunction(ctx, fn, desiredReplicas)
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
	fn, err := function.FromHeaders(req)
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
		controllerClient, ok := c.controllerClients.Load(controllerIP)
		if !ok {
			controllerClient = NewClient(controllerIP, FlagPort.Value)
			c.controllerClients.Store(controllerIP, controllerClient)
		}
		err = controllerClient.Assign(req.Context(), fn)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	_, assignmentInProgress := c.assignmentLock.LoadOrStore(fn, struct{}{})
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
			c.assignmentLock.Delete(fn)
		}()
	}()

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

func (c *Controller) getFunctionMetrics(ctx context.Context, namespace string) (map[function.Function]map[string]PodMetricsInfo, error) {
	pods, err := c.podManager.GetAllAssignedPods(namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get all assigned pods: %w", err)
	}

	podMetricsList, err := c.metricsClientset.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: key.Tenant.Label,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod metrics: %w", err)
	}

	metricsMap := make(map[string]metricsv1beta1.PodMetrics)
	for _, m := range podMetricsList.Items {
		metricsMap[m.Name] = m
	}

	functionsMap := make(map[function.Function]map[string]PodMetricsInfo)

	for _, pod := range pods {
		fn, err := function.FromPod(pod)
		if err != nil {
			log.Warn(ctx, "failed to get function from labels", key.Error.Field(err), slog.String("pod", pod.Name), slog.Any("labels", pod.Labels))
			continue
		}

		info := PodMetricsInfo{
			Pod:               pod,
			Ready:             false,
			AssignedAt:        fn.AssignedAt,
			DeletionTimestamp: pod.DeletionTimestamp,
		}

		if !fn.ReadyAt.IsZero() {
			for _, cond := range pod.Status.Conditions {
				if cond.Type == v1.PodReady && cond.Status == v1.ConditionTrue {
					info.Ready = true
					break
				}
			}
		}

		if m, exists := metricsMap[pod.Name]; exists {
			for _, c := range m.Containers {
				if c.Usage.Cpu() != nil {
					cpuUsage := c.Usage.Cpu().MilliValue()
					if info.CPUUsage == nil {
						info.CPUUsage = new(int64)
					}
					*info.CPUUsage += cpuUsage
				}
				if c.Usage.Memory() != nil {
					memUsage := c.Usage.Memory().Value()
					if info.MemoryUsage == nil {
						info.MemoryUsage = new(int64)
					}
					*info.MemoryUsage += memUsage
				}
			}
		} else {
			// Metrics missing for this pod
			info.CPUUsage = nil
			info.MemoryUsage = nil
		}

		if _, exists := functionsMap[fn.Function]; !exists {
			functionsMap[fn.Function] = make(map[string]PodMetricsInfo)
		}

		functionsMap[fn.Function][pod.Name] = info
	}

	return functionsMap, nil
}

func (c *Controller) scaleFunction(ctx context.Context, fn function.Function, desiredReplicas int) error {
	assignedPods, err := c.podManager.GetAssignedAndPending(fn)
	if err != nil {
		return fmt.Errorf("failed to get assigned pods: %w", err)
	}

	currentReplicas := len(assignedPods)
	if currentReplicas == desiredReplicas {
		return nil
	}

	log.Info(ctx, "scaling function", slog.Int("currentReplicas", currentReplicas), slog.Int("desiredReplicas", desiredReplicas), key.Function.Field(fn))

	if desiredReplicas > currentReplicas {
		// TODO: lock assigning map
		for i := 0; i < desiredReplicas-currentReplicas; i++ {
			pod, err := c.podManager.Assign(ctx, fn)
			if err != nil {
				return fmt.Errorf("failed to assign pod: %w", err)
			}
			log.Trace(ctx, "assigned pod", slog.Any("pod", pod.Name), key.Function.Field(fn))
		}
	} else {
		slices.SortFunc(assignedPods, func(a, b *v1.Pod) int {
			if a.DeletionTimestamp != nil && b.DeletionTimestamp == nil {
				return 1
			}

			if a.DeletionTimestamp == nil && b.DeletionTimestamp != nil {
				return -1
			}

			instanceA, err := function.FromPod(a)
			if err != nil {
				log.Warn(ctx, "failed to get function from labels", key.Error.Field(err), slog.String("pod", a.Name), slog.Any("labels", a.Labels))
				return -1
			}

			instanceB, err := function.FromPod(b)
			if err != nil {
				log.Warn(ctx, "failed to get function from labels", key.Error.Field(err), slog.String("pod", b.Name), slog.Any("labels", b.Labels))
				return 1
			}

			return instanceA.AssignedAt.Compare(instanceB.AssignedAt)
		})

		for i := 0; i < currentReplicas-desiredReplicas; i++ {
			pod := assignedPods[i]
			err := c.podManager.Terminate(ctx, fn, pod)
			if err != nil {
				return fmt.Errorf("failed to delete pod: %w", err)
			}
			log.Trace(ctx, "deleted pod", slog.Any("pod", pod.Name), key.Function.Field(fn))
		}
	}

	return nil
}
