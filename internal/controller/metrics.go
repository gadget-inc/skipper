package controller

import (
	"time"

	"github.com/gadget-inc/skipper/internal/metric"
	"github.com/prometheus/client_golang/prometheus"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

// metrics holds every prometheus metric the controller package
// registers, captured for the docs `{{< metricsTable controller >}}`
// shortcode. Help strings are doc-quality (single sentence, optional
// trailing parenthetical, <= 120 chars) -- the same text both
// scrapers and the rendered docs see.
var metrics = metric.NewSet(prometheus.DefaultRegisterer)

var (
	waitingForUnassignedPods = metrics.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "skipper",
		Subsystem: "controller",
		Name:      "waiting_for_unassigned_pods",
		Help:      "Functions blocked waiting for pod assignment.",
	}, []string{"function_deployment"})

	assignmentsTotal = metrics.NewCounterVec(prometheus.CounterOpts{
		Namespace: "skipper",
		Subsystem: "controller",
		Name:      "assignments_total",
		Help:      "Total pod assignments performed by the controller.",
	}, []string{"function_deployment"})

	scaleUpsTotal = metrics.NewCounterVec(prometheus.CounterOpts{
		Namespace: "skipper",
		Subsystem: "controller",
		Name:      "scale_ups_total",
		Help:      "Total scale-up operations performed by the controller.",
	}, []string{"function_deployment"})

	scaleDownsTotal = metrics.NewCounterVec(prometheus.CounterOpts{
		Namespace: "skipper",
		Subsystem: "controller",
		Name:      "scale_downs_total",
		Help:      "Total scale-down operations performed by the controller.",
	}, []string{"function_deployment"})

	informerEventsTotal = metrics.NewCounterVec(prometheus.CounterOpts{
		Namespace: "skipper",
		Subsystem: "controller",
		Name:      "informer_events_total",
		Help:      "Kubernetes informer events processed by the controller.",
	}, []string{"resource", "event"})

	informerEventLag = metrics.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "skipper",
		Subsystem: "controller",
		Name:      "informer_event_lag_seconds",
		Help:      "Lag between Kubernetes object change and informer processing (add and delete events only).",
		Buckets:   []float64{0.0625, 0.125, 0.25, 0.5, 1, 2, 4, 8, 16, 32, 64, 128, 256, 512},
	}, []string{"resource", "event"})

	informerLastEventTime = metrics.NewGauge(prometheus.GaugeOpts{
		Namespace: "skipper",
		Subsystem: "controller",
		Name:      "informer_last_event_time_seconds",
		Help:      "Unix timestamp of the last informer event processed.",
	})
)

// Metrics returns the metadata of every prometheus metric registered
// by the controller package. The docs site reads this slice via the
// `{{< metricsTable controller >}}` shortcode.
func Metrics() []metric.Metric {
	return metrics.Slice()
}

// InformerEventObject constrains the types that can be passed to RecordInformerEvent
// to exactly the types that our Kubernetes informers actually emit.
type InformerEventObject interface {
	*v1.Pod | *appsv1.ReplicaSet | cache.DeletedFinalStateUnknown | *cache.DeletedFinalStateUnknown
}

// RecordInformerEvent records metrics for Kubernetes informer events.
// The generic type constraint ensures only valid informer event types can be passed.
func RecordInformerEvent[T InformerEventObject](resource, event string, obj T) {
	informerEventsTotal.WithLabelValues(resource, event).Inc()
	informerLastEventTime.SetToCurrentTime()

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
