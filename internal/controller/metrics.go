package controller

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

var (
	waitingForUnassignedPods = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "skipper",
		Subsystem: "controller",
		Name:      "waiting_for_unassigned_pods",
		Help:      "The number of functions that are waiting for an unassigned pod",
	}, []string{"function_deployment"})

	assignmentsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "skipper",
		Subsystem: "controller",
		Name:      "assignments_total",
		Help:      "The number of times the controller has assigned a pod to a function",
	}, []string{"function_deployment"})

	scaleUpsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "skipper",
		Subsystem: "controller",
		Name:      "scale_ups_total",
		Help:      "The number of times the controller has scaled up a function",
	}, []string{"function_deployment"})

	scaleDownsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "skipper",
		Subsystem: "controller",
		Name:      "scale_downs_total",
		Help:      "The number of times the controller has scaled down a function",
	}, []string{"function_deployment"})

	informerEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "skipper",
		Subsystem: "controller",
		Name:      "informer_events_total",
		Help:      "Total number of informer events processed by the Skipper controller.",
	}, []string{"resource", "event"})

	informerEventLag = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "skipper",
		Subsystem: "controller",
		Name:      "informer_event_lag_seconds",
		Help:      "Age of Kubernetes objects when processed by Skipper controller informers.",
		Buckets:   []float64{0.0625, 0.125, 0.25, 0.5, 1, 2, 4, 8, 16, 32, 64, 128, 256, 512},
	}, []string{"resource", "event"})
)

// InformerEventObject constrains the types that can be passed to RecordInformerEvent
// to exactly the types that our Kubernetes informers actually emit.
type InformerEventObject interface {
	*v1.Pod | *appsv1.ReplicaSet | cache.DeletedFinalStateUnknown | *cache.DeletedFinalStateUnknown
}

// RecordInformerEvent records metrics for Kubernetes informer events.
// The generic type constraint ensures only valid informer event types can be passed.
func RecordInformerEvent[T InformerEventObject](resource, event string, obj T) {
	informerEventsTotal.WithLabelValues(resource, event).Inc()

	eventTime, ok := informerEventTimestamp(obj)
	if !ok {
		return
	}

	lag := time.Since(eventTime)
	if lag < 0 {
		lag = 0
	}

	informerEventLag.WithLabelValues(resource, event).Observe(lag.Seconds())
}

func informerEventTimestamp(obj any) (time.Time, bool) {
	metaObj, ok := metaAccessor(obj)
	if !ok {
		return time.Time{}, false
	}

	if ts := metaObj.GetDeletionTimestamp(); ts != nil {
		return ts.Time, true
	}

	if latest := latestManagedFieldsTime(metaObj.GetManagedFields()); !latest.IsZero() {
		return latest, true
	}

	creation := metaObj.GetCreationTimestamp()
	if !creation.IsZero() {
		return creation.Time, true
	}

	return time.Time{}, false
}

func metaAccessor(obj any) (metav1.Object, bool) {
	switch o := obj.(type) {
	case *v1.Pod:
		return o, true
	case *appsv1.ReplicaSet:
		return o, true
	case cache.DeletedFinalStateUnknown:
		return metaAccessor(o.Obj)
	case *cache.DeletedFinalStateUnknown:
		return metaAccessor(o.Obj)
	default:
		return nil, false
	}
}

func latestManagedFieldsTime(entries []metav1.ManagedFieldsEntry) time.Time {
	var latest time.Time
	for i := range entries {
		if entries[i].Time == nil {
			continue
		}
		if entries[i].Time.After(latest) {
			latest = entries[i].Time.Time
		}
	}
	return latest
}
