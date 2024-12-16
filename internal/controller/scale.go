package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	"github.com/gadget-inc/fusion/internal/timer"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

func (c *Controller) getFunctionMetrics(ctx context.Context, namespace string) (map[function.Function][]InstanceMetric, error) {
	pods, err := c.listPods(namespace, hasTenantSelector)
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
			log.Warn(ctx, "failed to get instance from labels", key.Error.Field(err), key.Pod.Field(pod), key.Labels.Field(pod.Labels))
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

func (c *Controller) scaleFunction(ctx context.Context, fn function.Function, desiredInstances int) ([]*function.Instance, error) {
	scaleMu, _ := c.scaleMu.LoadOrCompute(fn, func() *sync.Mutex { return new(sync.Mutex) })
	scaleMu.Lock()
	defer scaleMu.Unlock()

	assignedPods, err := c.listPods(fn.Namespace, labels.SelectorFromSet(labels.Set{
		key.Tenant.Label:     fn.Tenant,
		key.Deployment.Label: fn.Deployment,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to get assigned pods: %w", err)
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

	controllerIP, ok := c.ring.Get(fn.RingKey())
	if !ok {
		return nil, fmt.Errorf("no controller for function")
	}

	if controllerIP != FlagIP.Value() {
		log.Debug(ctx, "forwarding scale request", key.Function.Field(fn), key.ControllerIP.Field(controllerIP))
		controllerClient, _ := c.controllerClients.LoadOrCompute(controllerIP, func() Client { return NewHTTPClient(controllerIP, FlagPort.Value()) })
		return controllerClient.Scale(ctx, fn, desiredInstances)
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
			log.Trace(ctx, "no available pods", key.Function.Field(fn))
			return nil, nil
		}
		return availablePods[rand.Intn(len(availablePods))], nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to poll for available pod: %w", err)
	}

	assignPatches := []patchOperation{
		{Op: "test", Path: "/metadata/ownerReferences/0/kind", Value: "ReplicaSet"},
		{Op: "copy", From: "/metadata/ownerReferences/0/name", Path: key.ReplicaSet.PatchLabel},
		{Op: "replace", Path: key.Status.PatchLabel, Value: StatusPending},
		{Op: "add", Path: key.Tenant.PatchLabel, Value: fn.Tenant},
		{Op: "add", Path: key.Namespace.PatchLabel, Value: fn.Namespace},
		{Op: "add", Path: key.Deployment.PatchLabel, Value: fn.Deployment},
		{Op: "add", Path: key.MinInstances.PatchLabel, Value: strconv.Itoa(fn.MinInstances)},
		{Op: "add", Path: key.MaxInstances.PatchLabel, Value: strconv.Itoa(fn.MaxInstances)},
		{Op: "add", Path: key.TargetCPUUtilization.PatchLabel, Value: strconv.Itoa(fn.TargetCPUUtilization)},
		{Op: "add", Path: key.TargetMemoryUtilization.PatchLabel, Value: strconv.Itoa(fn.TargetMemoryUtilization)},
		{Op: "add", Path: key.AssignedAt.PatchLabel, Value: strconv.FormatInt(time.Now().Unix(), 10)},
	}

	patchBody, err := json.Marshal(assignPatches)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal assign patch: %w", err)
	}

	_, err = c.clientset.CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name, types.JSONPatchType, patchBody, metav1.PatchOptions{FieldManager: key.Controller.Label})
	if err != nil {
		return nil, fmt.Errorf("failed to assign pod: %w", err)
	}

	assignCtx, cancel := context.WithTimeout(ctx, function.FlagAssignTimeout.Value())
	defer cancel()

	assignURL := "http://" + pod.Status.PodIP + ":" + strconv.Itoa(function.FlagPort.Value()) + function.FlagAssignPath.Value()
	req, err := http.NewRequestWithContext(assignCtx, http.MethodPost, assignURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create assign request: %w", err)
	}

	req.Header.Set(key.Tenant.Header, fn.Tenant)
	req.Header.Set(key.Metadata.Header, fn.Metadata)

	log.Info(ctx, "assigning pod", key.Pod.Field(pod), key.Function.Field(fn))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send assign request: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("assign request failed: status=%d body=%s", res.StatusCode, getResponseBody(res))
	}

	setReadyPatches := []patchOperation{
		{Op: "replace", Path: key.Status.PatchLabel, Value: StatusReady},
		{Op: "add", Path: key.ReadyAt.PatchLabel, Value: strconv.FormatInt(time.Now().Unix(), 10)},
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
