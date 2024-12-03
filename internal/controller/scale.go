package controller

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	"github.com/gadget-inc/fusion/internal/timer"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

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
			pods, err := c.podManager.ListPods(namespace, hasTenantSelector)
			if err != nil {
				log.Warn(ctx, "failed to get all assigned pods for replica set check", key.Error.Field(err))
				return nil
			}

			var defunctInstances []function.Instance
			for _, pod := range pods {
				instance, err := function.FromPod(pod)
				if err != nil {
					log.Warn(ctx, "failed to get function from pod", key.Error.Field(err), key.Pod.Field(pod))
					continue
				}

				replicaSet, err := replicaSetLister.ReplicaSets(pod.Namespace).Get(instance.ReplicaSet)
				if err != nil {
					log.Warn(ctx, "failed to get replica set for pod", key.Pod.Field(pod), key.Error.Field(err))
					continue
				}

				if replicaSet.Spec.Replicas == nil || *replicaSet.Spec.Replicas == 0 {
					defunctInstances = append(defunctInstances, instance)
				}
			}

			for _, instance := range defunctInstances {
				log.Debug(ctx, "terminating defunct function", key.Pod.Field(instance.Pod), key.Function.Field(instance))
				err = c.clientset.CoreV1().Pods(instance.Pod.Namespace).Delete(ctx, instance.Pod.Name, metav1.DeleteOptions{})
				if err != nil {
					log.Warn(ctx, "failed to terminate pod", key.Error.Field(err), key.Pod.Field(instance.Pod), key.Function.Field(instance))
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
			c.keepAlivesMu.Lock()
			keepAlives := maps.Clone(c.keepAlives)
			c.keepAlivesMu.Unlock()

			for _, namespace := range function.FlagNamespaces.Value {
				functionMetrics, err := c.getFunctionMetrics(ctx, namespace)
				if err != nil {
					log.Warn(ctx, "failed to get function metrics", key.Error.Field(err))
					return nil
				}

				now := time.Now()
				for fn, instanceMetrics := range functionMetrics {
					timestamp, ok := keepAlives[fn]
					if !ok {
						log.Warn(ctx, "no keep alive for function", key.Function.Field(fn))
						for _, instanceMetric := range instanceMetrics {
							if instanceMetric.AssignedAt.After(timestamp) {
								timestamp = instanceMetric.AssignedAt
							}
						}
					}

					if time.Since(timestamp) > 90*time.Second {
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

						log.Trace(ctx, "scaling function to 0", key.Function.Field(fn), key.LastRequest.Field(timestamp))
						err := c.scaleFunction(ctx, fn, 0)
						if err != nil {
							log.Warn(ctx, "failed to scale function", key.Error.Field(err), key.Function.Field(fn))
						}
						continue
					}

					currentReplicas := len(instanceMetrics)
					desiredReplicas, err := calculateDesiredReplicas(
						currentReplicas,
						instanceMetrics,
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

func (c *Controller) getFunctionMetrics(ctx context.Context, namespace string) (map[function.Function][]InstanceMetric, error) {
	pods, err := c.podManager.ListPods(namespace, hasTenantSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to get all assigned pods: %w", err)
	}

	podMetricsList, err := c.metricsClientset.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: key.Tenant.Label,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod metrics: %w", err)
	}

	podMetricsMap := make(map[string]metricsv1beta1.PodMetrics)
	for _, podMetric := range podMetricsList.Items {
		podMetricsMap[podMetric.Name] = podMetric
	}

	functionMetrics := make(map[function.Function][]InstanceMetric)

	for _, pod := range pods {
		instance, err := function.FromPod(pod)
		if err != nil {
			log.Warn(ctx, "failed to get function from labels", key.Error.Field(err), key.Pod.Field(pod), slog.Any("labels", pod.Labels))
			continue
		}

		instanceMetric := InstanceMetric{Instance: instance}

		if m, exists := podMetricsMap[pod.Name]; exists {
			for _, container := range m.Containers {
				if container.Usage.Cpu() != nil {
					cpuUsage := container.Usage.Cpu().MilliValue()
					if instanceMetric.CPUUsage == nil {
						instanceMetric.CPUUsage = new(int64)
					}
					*instanceMetric.CPUUsage += cpuUsage
				}

				if container.Usage.Memory() != nil {
					memUsage := container.Usage.Memory().Value()
					if instanceMetric.MemoryUsage == nil {
						instanceMetric.MemoryUsage = new(int64)
					}
					*instanceMetric.MemoryUsage += memUsage
				}
			}
		} else {
			// metrics missing for this instance
			instanceMetric.CPUUsage = nil
			instanceMetric.MemoryUsage = nil
		}

		functionMetrics[instance.Function] = append(functionMetrics[instance.Function], instanceMetric)
	}

	return functionMetrics, nil
}

func (c *Controller) scaleFunction(ctx context.Context, fn function.Function, desiredReplicas int) error {
	assignedPods, err := c.podManager.ListPods(fn.Namespace, labels.SelectorFromSet(labels.Set{
		key.Tenant.Label:     fn.Tenant,
		key.Deployment.Label: fn.Deployment,
	}))
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
			pod, err := c.assign(ctx, fn)
			if err != nil {
				return fmt.Errorf("failed to assign pod: %w", err)
			}
			log.Trace(ctx, "assigned pod", slog.Any("pod", pod.Name), key.Function.Field(fn))
		}
	} else {
		slices.SortFunc(assignedPods, func(a, b *v1.Pod) int {
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
			err := c.clientset.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
			if err != nil {
				return fmt.Errorf("failed to delete pod: %w", err)
			}
			log.Trace(ctx, "deleted pod", slog.Any("pod", pod.Name), key.Function.Field(fn))
		}
	}

	return nil
}
