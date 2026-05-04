package metric_test

import (
	"testing"

	"github.com/gadget-inc/skipper/internal/metric"
	"github.com/prometheus/client_golang/prometheus"
	"gotest.tools/v3/assert"
)

func TestSetCapturesMetadataAcrossAllConstructors(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	s := metric.NewSet(reg)

	_ = s.NewCounterVec(prometheus.CounterOpts{
		Namespace: "test", Subsystem: "set", Name: "cv_total", Help: "counter vec.",
	}, []string{"label"})
	_ = s.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "test", Subsystem: "set", Name: "gv", Help: "gauge vec.",
	}, []string{"label"})
	_ = s.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "test", Subsystem: "set", Name: "hv_seconds", Help: "histogram vec.",
		Buckets: []float64{0.1, 1, 10},
	}, []string{"label"})
	_ = s.NewCounter(prometheus.CounterOpts{
		Namespace: "test", Subsystem: "set", Name: "c_total", Help: "counter.",
	})
	_ = s.NewGauge(prometheus.GaugeOpts{
		Namespace: "test", Subsystem: "set", Name: "g", Help: "gauge.",
	})
	_ = s.NewHistogram(prometheus.HistogramOpts{
		Namespace: "test", Subsystem: "set", Name: "h", Help: "histogram.",
		Buckets: []float64{1, 2, 3},
	})

	got := s.Slice()
	assert.Equal(t, len(got), 6)

	byName := map[string]metric.Metric{}
	for _, m := range got {
		byName[m.Name] = m
	}

	cv := byName["cv_total"]
	assert.Equal(t, cv.Type, metric.TypeCounter)
	assert.DeepEqual(t, cv.Labels, []string{"label"})
	assert.Equal(t, cv.Help, "counter vec.")
	assert.Assert(t, cv.Buckets == nil)

	gv := byName["gv"]
	assert.Equal(t, gv.Type, metric.TypeGauge)
	assert.DeepEqual(t, gv.Labels, []string{"label"})
	assert.Assert(t, gv.Buckets == nil)

	hv := byName["hv_seconds"]
	assert.Equal(t, hv.Type, metric.TypeHistogram)
	assert.DeepEqual(t, hv.Labels, []string{"label"})
	assert.DeepEqual(t, hv.Buckets, []float64{0.1, 1, 10})

	c := byName["c_total"]
	assert.Equal(t, c.Type, metric.TypeCounter)
	assert.Assert(t, c.Labels == nil)

	g := byName["g"]
	assert.Equal(t, g.Type, metric.TypeGauge)
	assert.Assert(t, g.Labels == nil)

	h := byName["h"]
	assert.Equal(t, h.Type, metric.TypeHistogram)
	assert.Assert(t, h.Labels == nil)
	assert.DeepEqual(t, h.Buckets, []float64{1, 2, 3})
}
