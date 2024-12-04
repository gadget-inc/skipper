package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"strconv"
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
)

type patchOperation struct {
	Op    string `json:"op"`
	From  string `json:"from,omitempty"`
	Path  string `json:"path"`
	Value string `json:"value,omitempty"`
}

func (c *Controller) getAvailablePodsForFunction(fn function.Function) ([]*v1.Pod, error) {
	equalDeploymentName, err := labels.NewRequirement(key.Deployment.Label, selection.Equals, []string{fn.Deployment})
	if err != nil {
		return nil, err
	}

	pods, err := c.podManager.ListPods(fn.Namespace, doesNotHaveTenantSelector.Add(*equalDeploymentName))
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

func (c *Controller) assign(ctx context.Context, fn function.Function) (*v1.Pod, error) {
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
		{Op: "add", Path: key.MinReplicas.PatchLabel, Value: fn.MinReplicasStr},
		{Op: "add", Path: key.MaxReplicas.PatchLabel, Value: fn.MaxReplicasStr},
		{Op: "add", Path: key.TargetCPUUtilization.PatchLabel, Value: fn.TargetCPUUtilizationStr},
		{Op: "add", Path: key.TargetMemoryUtilization.PatchLabel, Value: fn.TargetMemoryUtilizationStr},
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

	assignCtx, cancel := context.WithTimeout(ctx, function.FlagAssignTimeout.Value)
	defer cancel()

	assignURL := fmt.Sprintf("http://%s:%d%s", pod.Status.PodIP, function.FlagPort.Value, function.FlagAssignPath.Value)
	req, err := http.NewRequestWithContext(assignCtx, http.MethodPost, assignURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create assign request: %w", err)
	}

	req.Header.Set(key.Tenant.Header, fn.Tenant)
	req.Header.Set(key.Metadata.Header, fn.Metadata)

	log.Info(ctx, "assigning pod", slog.Any("pod", pod.Name), key.Function.Field(fn))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send assign request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// TODO: log response body
		return nil, fmt.Errorf("assign request returned status %d", resp.StatusCode)
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

	return pod, nil
}
