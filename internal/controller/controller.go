package controller

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/hashring"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	"github.com/gadget-inc/fusion/internal/timer"
	"github.com/puzpuzpuz/xsync/v3"
	appsv1 "k8s.io/api/apps/v1"
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
	podListerMap      map[string]podListerEntry
	controllerClients *xsync.MapOf[string, Client]
	scaleMu           *xsync.MapOf[function.Function, *sync.Mutex]
	heartbeats        map[function.Function]time.Time
	heartbeatsMu      sync.Mutex // guards heartbeats
}

func New(clientset kubernetes.Interface, metricsClient metricsclientset.Interface) *Controller {
	return &Controller{
		ring:              hashring.New(),
		clientset:         clientset,
		metricsClientset:  metricsClient,
		podListerMap:      make(map[string]podListerEntry, len(function.FlagNamespaces.Value())),
		controllerClients: xsync.NewMapOf[string, Client](),
		scaleMu:           xsync.NewMapOf[function.Function, *sync.Mutex](),
		heartbeats:        make(map[function.Function]time.Time),
	}
}

func (c *Controller) Start(ctx context.Context) error {
	err := c.startControllerInformer(ctx)
	if err != nil {
		return fmt.Errorf("failed to start controller pod informer: %w", err)
	}
	err = c.startPodInformers(ctx)
	if err != nil {
		return fmt.Errorf("failed to start managed pod informers: %w", err)
	}
	err = c.startReplicaSetInformer(ctx)
	if err != nil {
		return fmt.Errorf("failed to start managed replica set informer: %w", err)
	}
	err = c.startScalingInstances(ctx)
	if err != nil {
		return fmt.Errorf("failed to start scaling tenant pods: %w", err)
	}
	return nil
}

func (c *Controller) startControllerInformer(ctx context.Context) error {
	controllerPodInformerFactory := informers.NewSharedInformerFactoryWithOptions(
		c.clientset,
		10*time.Minute,
		informers.WithNamespace(FlagNamespace.Value()),
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
			log.Trace(ctx, "controller ips", key.ControllerIPs.Field(c.ring.List()))
			return nil
		})
	}()

	return nil
}

func (c *Controller) startReplicaSetInformer(ctx context.Context) error {
	log.Info(ctx, "starting managed replica set informers", key.Namespaces.Field(function.FlagNamespaces.Value()))

	for _, namespace := range function.FlagNamespaces.Value() {
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
			pods, err := c.listPods(namespace, hasTenantSelector)
			if err != nil {
				log.Warn(ctx, "failed to get all assigned pods for replica set check", key.Error.Field(err))
				return nil
			}

			var defunctInstances []*function.Instance
			for _, pod := range pods {
				instance, err := function.FromPod(pod)
				if err != nil {
					log.Warn(ctx, "failed to get function from pod", key.Error.Field(err), key.Pod.Field(pod))
					continue
				}

				replicaSet, err := replicaSetLister.ReplicaSets(pod.Namespace).Get(instance.Version)
				if err != nil {
					log.Warn(ctx, "failed to get replica set for pod", key.Error.Field(err), key.Pod.Field(pod))
					continue
				}

				if replicaSet.Spec.Replicas == nil || *replicaSet.Spec.Replicas == 0 {
					defunctInstances = append(defunctInstances, instance)
				}
			}

			for _, instance := range defunctInstances {
				func() {
					scaleMu, _ := c.scaleMu.LoadOrCompute(instance.Function, func() *sync.Mutex { return new(sync.Mutex) })
					scaleMu.Lock()
					defer scaleMu.Unlock()

					log.Debug(ctx, "terminating defunct function", key.Instance.Field(instance))
					err = c.clientset.CoreV1().Pods(instance.Namespace).Delete(ctx, instance.Name, metav1.DeleteOptions{})
					if err != nil {
						log.Warn(ctx, "failed to terminate instance", key.Error.Field(err), key.Instance.Field(instance))
					}
				}()

				time.Sleep(1 * time.Second)
			}

			return nil
		})
	}

	return nil
}

func (c *Controller) startScalingInstances(ctx context.Context) error {
	// TODO: garbage collect old stabilization windows
	stabilizationWindows := make(map[function.Function]*StabilizationWindow)

	go timer.Loop(
		ctx,
		15*time.Second,
		func(ctx context.Context) error {
			c.heartbeatsMu.Lock()
			heartbeats := maps.Clone(c.heartbeats)
			c.heartbeatsMu.Unlock()

			for _, namespace := range function.FlagNamespaces.Value() {
				functionMetrics, err := c.getFunctionMetrics(ctx, namespace)
				if err != nil {
					log.Warn(ctx, "failed to get function metrics", key.Error.Field(err))
					return nil
				}

				now := time.Now()
				for fn, instanceMetrics := range functionMetrics {
					timestamp, ok := heartbeats[fn]
					if !ok {
						log.Warn(ctx, "no heartbeat for function", key.Function.Field(fn))
						for _, instanceMetric := range instanceMetrics {
							if instanceMetric.AssignedAt.After(timestamp) {
								timestamp = instanceMetric.AssignedAt
							}
						}
					}

					if time.Since(timestamp) > 90*time.Second {
						delete(stabilizationWindows, fn)

						controllerIP, ok := c.ring.Get(fn.RingKey())
						if !ok || controllerIP != FlagIP.Value() {
							log.Trace(ctx, "skipping scaling fn to 0, not assigned to this controller",
								key.Function.Field(fn),
								key.ControllerIP.Field(controllerIP),
								key.IP.Field(FlagIP.Value()),
								slog.Bool("ok", ok),
							)
							continue
						}

						log.Trace(ctx, "scaling function to 0", key.Function.Field(fn), key.Timestamp.Field(timestamp))
						_, err := c.scaleFunction(ctx, fn, 0)
						if err != nil {
							log.Warn(ctx, "failed to scale function", key.Error.Field(err), key.Function.Field(fn))
						}
						continue
					}

					currentInstances := len(instanceMetrics)
					desiredInstances, err := calculateDesiredInstances(
						currentInstances,
						instanceMetrics,
						int64(fn.TargetCPUUtilization),
						// int64(fn.TargetMemoryUtilization),
						DefaultConfig,
						now,
					)
					if err != nil {
						log.Trace(ctx, "failed to calculate desired instances", key.Error.Field(err), key.Function.Field(fn))
						continue
					}

					if desiredInstances < fn.MinInstances {
						desiredInstances = fn.MinInstances
					}

					if desiredInstances > fn.MaxInstances {
						desiredInstances = fn.MaxInstances
					}

					stabilizationWindow, exists := stabilizationWindows[fn]
					if !exists {
						stabilizationWindow = &StabilizationWindow{Window: DefaultConfig.DownscaleStabilization}
						stabilizationWindows[fn] = stabilizationWindow
					}

					log.Trace(ctx, "desired instances",
						key.Function.Field(fn),
						key.CurrentInstances.Field(currentInstances),
						key.DesiredInstances.Field(desiredInstances),
						key.MaxRecommendedInstances.Field(stabilizationWindow.GetMaxRecommendation()),
					)

					stabilizationWindow.RecordRecommendation(desiredInstances, now)

					controllerIP, ok := c.ring.Get(fn.RingKey())
					if !ok || controllerIP != FlagIP.Value() {
						log.Trace(ctx, "skipping scaling for function, not assigned to this controller",
							key.Function.Field(fn),
							key.Controller.Field(controllerIP),
							key.IP.Field(FlagIP.Value()),
							slog.Bool("ok", ok),
						)
						continue
					}

					if desiredInstances < currentInstances {
						maxRecommendedInstances := stabilizationWindow.GetMaxRecommendation()
						if maxRecommendedInstances < currentInstances {
							desiredInstances = maxRecommendedInstances
						} else {
							desiredInstances = currentInstances
						}
					}

					if desiredInstances == 0 {
						// we only scale to 0 if the last request was more than 90 seconds ago
						log.Debug(ctx, "skipping scaling function to 0 based on hpa", key.Function.Field(fn))
						continue
					}

					log.Trace(ctx, "scaling function",
						key.Function.Field(fn),
						key.CurrentInstances.Field(currentInstances),
						key.DesiredInstances.Field(desiredInstances),
						key.MaxRecommendedInstances.Field(stabilizationWindow.GetMaxRecommendation()),
					)

					_, err = c.scaleFunction(ctx, fn, desiredInstances)
					if err != nil {
						log.Warn(ctx, "failed to scale function",
							key.Error.Field(err),
							key.Function.Field(fn),
							key.CurrentInstances.Field(currentInstances),
							key.DesiredInstances.Field(desiredInstances),
						)
					}
				}
			}

			return nil
		},
	)

	return nil
}
