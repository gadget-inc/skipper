package router

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gadget-inc/fusion/internal/destination"
	"github.com/gadget-inc/fusion/internal/kubernetes"
	"github.com/gadget-inc/fusion/internal/timer"
)

type Router struct {
	assignmentLock sync.Map
	k8s            *kubernetes.Client
}

func New(k8s *kubernetes.Client) *Router {
	return &Router{k8s: k8s}
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	destination, err := destination.New(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := req.Context()
	pod, err := r.getPod(ctx, destination)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	pod.ServeHTTP(w, req)
}

func (r *Router) getPod(ctx context.Context, dest destination.Destination) (*kubernetes.Pod, error) {
	return timer.Poll(ctx, 100*time.Millisecond, 5*time.Second, func(ctx context.Context) (*kubernetes.Pod, error) {
		assignedPods, err := r.k8s.ListAssignedPods(ctx, dest)
		if err != nil {
			return nil, fmt.Errorf("failed to list assigned pods: %w", err)
		}
		if len(assignedPods) > 0 {
			return assignedPods[rand.Intn(len(assignedPods))], nil
		}

		_, assignmentInProgress := r.assignmentLock.LoadOrStore(dest.String(), struct{}{})
		if assignmentInProgress {
			// another goroutine is already trying to assign a pod for this destination
			return nil, nil
		}
		defer r.assignmentLock.Delete(dest.String())

		availablePods, err := r.k8s.ListAvailablePods(ctx, dest)
		if err != nil {
			return nil, fmt.Errorf("failed to list available pods: %w", err)
		}
		if len(availablePods) == 0 {
			slog.WarnContext(ctx, "no available pods", slog.Any("destination", dest))
			return nil, nil
		}

		for _, pod := range availablePods {
			err := r.k8s.AssignPod(ctx, pod, dest)
			if err != nil {
				slog.ErrorContext(ctx, "failed to assign pod", slog.Any("error", err), slog.Any("destination", dest))
				continue
			}
			return pod, nil
		}

		return nil, nil
	})
}
