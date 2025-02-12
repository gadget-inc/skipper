package controller

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"math/rand"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"aidanwoods.dev/go-paseto"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	"github.com/gadget-inc/fusion/internal/timer"
	"github.com/goccy/go-json"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

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

						controllerIP := c.ring.Get(fn.RingKey())
						if controllerIP != FlagIP.Value() {
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

					controllerIP := c.ring.Get(fn.RingKey())
					if controllerIP != FlagIP.Value() {
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

func (c *Controller) getFunctionMetrics(ctx context.Context, namespace string) (map[function.Function][]InstanceMetric, error) {
	pods, err := c.listPods(namespace, hasTenantSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to get all assigned pods: %w", err)
	}

	metrics, err := c.metricsClientset.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{LabelSelector: key.Tenant.Label})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod metrics: %w", err)
	}

	podNameToMetric := make(map[string]metricsv1beta1.PodMetrics, len(metrics.Items))
	for _, metric := range metrics.Items {
		podNameToMetric[metric.Name] = metric
	}

	functionMetrics := make(map[function.Function][]InstanceMetric)

	for _, pod := range pods {
		instance, err := function.FromPod(pod)
		if err != nil {
			log.Warn(ctx, "failed to get instance from labels", key.Error.Field(err), key.Pod.Field(pod), key.Labels.Field(pod.Labels))
			continue
		}

		instanceMetric := InstanceMetric{Instance: instance}

		if m, exists := podNameToMetric[pod.Name]; exists {
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
		}

		functionMetrics[instance.Function] = append(functionMetrics[instance.Function], instanceMetric)
	}

	return functionMetrics, nil
}

func (c *Controller) scaleFunction(ctx context.Context, fn function.Function, desiredInstances int) ([]*function.Instance, error) {
	scaleMu, _ := c.scaleMu.LoadOrCompute(fn, func() *sync.Mutex { return new(sync.Mutex) })
	scaleMu.Lock()
	defer scaleMu.Unlock()

	assignedPods, err := c.listPods(fn.Namespace, labels.SelectorFromSet(labels.Set{
		key.Tenant.Label:     fn.Tenant,
		key.Deployment.Label: fn.Deployment,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to list assigned pods: %w", err)
	}

	var instances []*function.Instance
	for _, pod := range assignedPods {
		instance, err := function.FromPod(pod)
		if err != nil {
			return nil, fmt.Errorf("failed to get function from labels: %w", err)
		}
		instances = append(instances, instance)
	}

	currentInstances := len(instances)
	if currentInstances == desiredInstances {
		return instances, nil
	}

	controllerIP := c.ring.Get(fn.RingKey())
	if controllerIP != FlagIP.Value() {
		log.Debug(ctx, "forwarding scale request", key.Function.Field(fn), key.ControllerIP.Field(controllerIP))
		return c.getControllerClient(controllerIP).Scale(ctx, fn, desiredInstances)
	}

	log.Info(ctx, "scaling function",
		key.Function.Field(fn),
		key.CurrentInstances.Field(currentInstances),
		key.DesiredInstances.Field(desiredInstances),
	)

	if desiredInstances > currentInstances {
		for i := 0; i < desiredInstances-currentInstances; i++ {
			instance, err := c.assignPodToFunction(ctx, fn)
			if err != nil {
				return nil, fmt.Errorf("failed to assign pod: %w", err)
			}
			instances = append(instances, instance)
			log.Trace(ctx, "assigned pod", key.Instance.Field(instance))
		}
	} else {
		// sort instances by assigned at in ascending order
		slices.SortFunc(instances, func(a, b *function.Instance) int { return a.AssignedAt.Compare(b.AssignedAt) })

		// iterate over instances in reverse order, deleting the oldest ones first
		for i := len(instances) - 1; i >= desiredInstances; i-- {
			instance := instances[i]
			err := c.clientset.CoreV1().Pods(instance.Namespace).Delete(ctx, instance.Name, metav1.DeleteOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to delete pod: %w", err)
			}
			instances = instances[:i]
			log.Trace(ctx, "deleted pod", key.Instance.Field(instance))
		}
	}

	return instances, nil
}

func (c *Controller) assignPodToFunction(ctx context.Context, fn function.Function) (*function.Instance, error) {
	pod, err := timer.PollUntil(ctx, 250*time.Millisecond, func(ctx context.Context) (*v1.Pod, error) {
		availablePods, err := c.getAvailablePodsForFunction(fn)
		if err != nil {
			return nil, fmt.Errorf("failed to list available pods: %w", err)
		}
		if len(availablePods) == 0 {
			log.Warn(ctx, "no available pods", key.Function.Field(fn))
			return nil, nil
		}
		return availablePods[rand.Intn(len(availablePods))], nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to poll for available pod: %w", err)
	}

	fnBytes, err := json.Marshal(fn)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal function: %w", err)
	}

	assignPatches := []patchOperation{
		{Op: "test", Path: "/metadata/ownerReferences/0/kind", Value: "ReplicaSet"},                     // ensure the pod is assigned to a replica set
		{Op: "copy", From: "/metadata/ownerReferences/0/name", Path: key.ReplicaSet.PatchAnnotation},    // copy its name so we know which replica set the pod belonged to before it was assigned
		{Op: "add", Path: key.Tenant.PatchLabel, Value: fn.Tenant},                                      // label the pod with the tenant it belongs to
		{Op: "add", Path: key.Function.PatchAnnotation, Value: string(fnBytes)},                         // annotate the pod with the function it is assigned to
		{Op: "add", Path: key.AssignedAt.PatchAnnotation, Value: time.Now().UTC().Format(time.RFC3339)}, // annotate the pod with the time it was assigned
	}

	patchBody, err := json.Marshal(assignPatches)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal assign patch: %w", err)
	}

	pod, err = c.clientset.CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name, types.JSONPatchType, patchBody, metav1.PatchOptions{FieldManager: key.Controller.Label})
	if err != nil {
		// there are many reasons this can fail, but one hard to debug
		// one is that the pod doesn't have any annotations causing the
		// json patch to fail: https://github.com/kubernetes-sigs/kustomize/issues/2986#issuecomment-692891118
		return nil, fmt.Errorf("failed to patch pod: %w", err)
	}

	err = c.updatePodCache(pod)
	if err != nil {
		return nil, fmt.Errorf("failed to update pod: %w", err)
	}

	var port string
	for _, container := range pod.Spec.Containers {
		for _, containerPort := range container.Ports {
			port = strconv.Itoa(int(containerPort.ContainerPort))
			break
		}
	}
	if port == "" {
		return nil, fmt.Errorf("failed to get port for pod: %w", err)
	}

	assignURL := "http://" + pod.Status.PodIP + ":" + port + function.FlagAssignPath.Value()
	assignCtx, cancel := context.WithTimeout(ctx, function.FlagAssignTimeout.Value())
	defer cancel()

	token := paseto.NewToken()
	token.SetSubject(fn.Tenant)
	token.SetIssuedAt(time.Now())
	token.SetNotBefore(time.Now())
	token.SetExpiration(time.Now().Add(7 * 24 * time.Hour))

	req, err := http.NewRequestWithContext(assignCtx, http.MethodPost, assignURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create assign request: %w", err)
	}

	req.Header.Set(key.Token.Header, token.V2Sign(FlagPasetoPrivateKey.Value()))
	fn.SetHeader(req) // TODO: put the function in the token instead

	log.Info(ctx, "assigning pod", key.Pod.Field(pod), key.Function.Field(fn))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send assign request: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("assign request failed: status=%d body=%s", res.StatusCode, getResponseBody(res))
	}

	setReadyPatches := []patchOperation{
		{Op: "add", Path: key.ReadyAt.PatchAnnotation, Value: time.Now().UTC().Format(time.RFC3339)},
	}

	patchBody, err = json.Marshal(setReadyPatches)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal set ready patch: %w", err)
	}

	pod, err = c.clientset.CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name, types.JSONPatchType, patchBody, metav1.PatchOptions{FieldManager: key.Controller.Label})
	if err != nil {
		return nil, fmt.Errorf("failed to patch status: %w", err)
	}

	err = c.updatePodCache(pod)
	if err != nil {
		return nil, fmt.Errorf("failed to update pod: %w", err)
	}

	return function.FromPod(pod)
}

func (c *Controller) getAvailablePodsForFunction(fn function.Function) ([]*v1.Pod, error) {
	equalDeploymentName, err := labels.NewRequirement(key.Deployment.Label, selection.Equals, []string{fn.Deployment})
	if err != nil {
		return nil, err
	}

	pods, err := c.listPods(fn.Namespace, doesNotHaveTenantSelector.Add(*equalDeploymentName))
	if err != nil {
		return nil, err
	}

	var availablePods []*v1.Pod
	for _, pod := range pods {
		if pod.Status.PodIP != "" {
			for _, cond := range pod.Status.Conditions {
				if cond.Type == v1.PodReady && cond.Status == v1.ConditionTrue {
					availablePods = append(availablePods, pod)
				}
			}
		}
	}

	return availablePods, nil
}

type patchOperation struct {
	Op    string `json:"op"`
	From  string `json:"from,omitempty"`
	Path  string `json:"path"`
	Value string `json:"value,omitempty"`
}
