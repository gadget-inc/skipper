// Package metric is a thin wrapper around prometheus/promauto that
// captures each registered metric's metadata in a typed slice as a
// side effect of registration. The captured slice is the source of
// truth for the docs site's `{{< metricsTable >}}` shortcode, which
// renders the live metric set instead of a hand-typed duplicate.
//
// A package owning metrics constructs a [Set], registers each metric
// through one of the [Set.New*] constructors, and exposes
// [Set.Slice] (or a wrapping accessor) to the docs renderer. The
// constructors mirror the [promauto] API one-for-one and return the
// same live [prometheus.Collector] types, so call sites change only
// their receiver from `promauto` to the package's [Set].
package metric

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Type identifies a Prometheus metric kind.
type Type int

const (
	TypeCounter Type = iota
	TypeGauge
	TypeHistogram
)

// String returns the human-facing label used in rendered tables
// (`Counter`, `Gauge`, `Histogram`).
func (t Type) String() string {
	switch t {
	case TypeCounter:
		return "Counter"
	case TypeGauge:
		return "Gauge"
	case TypeHistogram:
		return "Histogram"
	default:
		return "Unknown"
	}
}

// Metric is the captured metadata for a single registered metric.
// Labels is nil for non-Vec metrics; Buckets is nil for non-histogram
// metrics. Name is the bare metric name (without Namespace/Subsystem
// prefixes), matching the form used in operator-facing docs.
type Metric struct {
	Name    string
	Type    Type
	Labels  []string
	Help    string
	Buckets []float64
}

// Set captures metric metadata while delegating registration to
// promauto. The zero value is not usable; construct via [NewSet].
type Set struct {
	factory promauto.Factory
	slice   []Metric
}

// NewSet returns a Set that registers metrics with reg. Pass
// [prometheus.DefaultRegisterer] to keep parity with the bare
// promauto API.
func NewSet(reg prometheus.Registerer) *Set {
	return &Set{factory: promauto.With(reg)}
}

// Slice returns the captured metrics in registration order.
func (s *Set) Slice() []Metric {
	return s.slice
}

// NewCounterVec mirrors [promauto.Factory.NewCounterVec] and
// captures the metric metadata.
func (s *Set) NewCounterVec(opts prometheus.CounterOpts, labelNames []string) *prometheus.CounterVec {
	s.slice = append(s.slice, Metric{
		Name:   opts.Name,
		Type:   TypeCounter,
		Labels: labelNames,
		Help:   opts.Help,
	})
	return s.factory.NewCounterVec(opts, labelNames)
}

// NewGaugeVec mirrors [promauto.Factory.NewGaugeVec] and captures
// the metric metadata.
func (s *Set) NewGaugeVec(opts prometheus.GaugeOpts, labelNames []string) *prometheus.GaugeVec {
	s.slice = append(s.slice, Metric{
		Name:   opts.Name,
		Type:   TypeGauge,
		Labels: labelNames,
		Help:   opts.Help,
	})
	return s.factory.NewGaugeVec(opts, labelNames)
}

// NewHistogramVec mirrors [promauto.Factory.NewHistogramVec] and
// captures the metric metadata.
func (s *Set) NewHistogramVec(opts prometheus.HistogramOpts, labelNames []string) *prometheus.HistogramVec {
	s.slice = append(s.slice, Metric{
		Name:    opts.Name,
		Type:    TypeHistogram,
		Labels:  labelNames,
		Help:    opts.Help,
		Buckets: opts.Buckets,
	})
	return s.factory.NewHistogramVec(opts, labelNames)
}

// NewCounter mirrors [promauto.Factory.NewCounter] and captures the
// metric metadata.
func (s *Set) NewCounter(opts prometheus.CounterOpts) prometheus.Counter {
	s.slice = append(s.slice, Metric{
		Name: opts.Name,
		Type: TypeCounter,
		Help: opts.Help,
	})
	return s.factory.NewCounter(opts)
}

// NewGauge mirrors [promauto.Factory.NewGauge] and captures the
// metric metadata.
func (s *Set) NewGauge(opts prometheus.GaugeOpts) prometheus.Gauge {
	s.slice = append(s.slice, Metric{
		Name: opts.Name,
		Type: TypeGauge,
		Help: opts.Help,
	})
	return s.factory.NewGauge(opts)
}

// NewHistogram mirrors [promauto.Factory.NewHistogram] and captures
// the metric metadata.
func (s *Set) NewHistogram(opts prometheus.HistogramOpts) prometheus.Histogram {
	s.slice = append(s.slice, Metric{
		Name:    opts.Name,
		Type:    TypeHistogram,
		Help:    opts.Help,
		Buckets: opts.Buckets,
	})
	return s.factory.NewHistogram(opts)
}
