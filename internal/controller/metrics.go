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
		Help:      "Time between Kubernetes object creation/deletion and informer processing. Only measured for add (creation timestamp) and delete (deletion timestamp) events; update events are not measured due to lack of reliable timestamps.",
		Buckets:   []float64{0.0625, 0.125, 0.25, 0.5, 1, 2, 4, 8, 16, 32, 64, 128, 256, 512},
	}, []string{"resource", "event"})

	informerLastEventTime = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "skipper",
		Subsystem: "controller",
		Name:      "informer_last_event_time_seconds",
		Help:      "Unix timestamp of the last informer event processed. Use (time() - metric) to calculate time since last event.",
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
	informerLastEventTime.WithLabelValues(resource, event).SetToCurrentTime()

	eventTime, ok := informerEventTimestamp(obj, event)
	if !ok {
		return
	}

	lag := max(0, time.Since(eventTime))
	informerEventLag.WithLabelValues(resource, event).Observe(lag.Seconds())
}

func informerEventTimestamp(obj any, eventType string) (time.Time, bool) {
	metaObj, ok := metaAccessor(obj)
	if !ok {
		return time.Time{}, false
	}

	switch eventType {
	case "delete":
		// For delete events, use the deletion timestamp
		if ts := metaObj.GetDeletionTimestamp(); ts != nil {
			return ts.Time, true
		}

	case "add":
		// For add events, use the creation timestamp
		creation := metaObj.GetCreationTimestamp()
		if !creation.IsZero() {
			return creation.Time, true
		}

	case "update":
		// Update events cannot be reliably measured for lag. There's no single timestamp
		// that represents "when this update happened" - updates can change metadata
		// (labels/annotations), spec, or status, and only some changes have associated
		// timestamps. Status condition transitions and container state changes have
		// timestamps, but these may not change on every update (e.g., metadata-only updates).
		// ManagedFields timestamps exist but represent the last time any field manager
		// touched the object, which could be hours or days ago for long-lived pods.
		// Rather than provide misleading metrics, we skip update events entirely.
		return time.Time{}, false
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
