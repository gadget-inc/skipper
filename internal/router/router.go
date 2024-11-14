package router

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/gadget-inc/fusion/internal/controller"
	"github.com/gadget-inc/fusion/internal/destination"
	"github.com/gadget-inc/fusion/internal/pod"
	"github.com/gadget-inc/fusion/internal/timer"
	"k8s.io/client-go/kubernetes"
)

type Router struct {
	controllerClient *controller.Client
	clientset        *kubernetes.Clientset
	podManager       *pod.Manager
}

func New(controllerClient *controller.Client, clientset *kubernetes.Clientset, podManager *pod.Manager) *Router {
	return &Router{controllerClient: controllerClient, clientset: clientset, podManager: podManager}
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	dest, err := destination.New(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := req.Context()
	pod, err := timer.Poll(ctx, 100*time.Millisecond, 5*time.Second, func(ctx context.Context) (*pod.Pod, error) {
		assignedPods, err := r.podManager.GetAssigned(dest)
		if err != nil {
			return nil, fmt.Errorf("failed to list assigned pods: %w", err)
		}
		if len(assignedPods) > 0 {
			return pod.New(assignedPods[rand.Intn(len(assignedPods))]), nil
		}
		return nil, r.controllerClient.Assign(ctx, dest)
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	pod.ServeHTTP(w, req)
}
