package telemetry_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/config"
	"github.com/gadget-inc/skipper/internal/controller"
	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/telemetry"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"gotest.tools/v3/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPrometheusMetricsEndpoints_Controller(t *testing.T) {
	cleanup, metricsURL := bootMetricsServer(t, "controller")
	t.Cleanup(cleanup)

	controller.RecordInformerEvent("test_resource", "add", &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "foo",
			Namespace:         fixture.FunctionNamespace,
			CreationTimestamp: metav1.NewTime(time.Now().Add(-30 * time.Second)),
		},
	})

	resp := scrapeMetrics(t, metricsURL)
	defer resp.Body.Close()

	assert.Assert(t, resp.StatusCode == http.StatusOK)

	body, err := io.ReadAll(resp.Body)
	assert.NilError(t, err)
	assert.Assert(t, len(body) > 0)

	parser := expfmt.NewTextParser(model.UTF8Validation)
	metrics, err := parser.TextToMetricFamilies(bytes.NewReader(body))
	assert.NilError(t, err)
	assert.Assert(t, len(metrics) > 0)

	assert.Assert(t, metrics["skipper_controller_informer_events_total"] != nil)
	assert.Assert(t, metrics["skipper_controller_informer_event_lag_seconds"] != nil)
}

func TestPrometheusMetricsEndpoints_Router(t *testing.T) {
	cleanup, metricsURL := bootMetricsServer(t, "router")
	t.Cleanup(cleanup)

	resp := scrapeMetrics(t, metricsURL)
	defer resp.Body.Close()

	assert.Assert(t, resp.StatusCode == http.StatusOK)

	body, err := io.ReadAll(resp.Body)
	assert.NilError(t, err)
	assert.Assert(t, len(body) > 0)

	parser := expfmt.NewTextParser(model.UTF8Validation)
	metrics, err := parser.TextToMetricFamilies(bytes.NewReader(body))
	assert.NilError(t, err)
	assert.Assert(t, len(metrics) > 0)
}

func bootMetricsServer(t *testing.T, component string) (func(), string) {
	t.Helper()

	cfg := config.New[telemetry.Config]()
	cfg.Enabled = true
	cfg.Metric = true
	cfg.Trace = false
	cfg.ShutdownTimeout = 2 * time.Second
	cfg.PrometheusHost = "127.0.0.1"
	cfg.MetricOTLP = false
	cfg.PrometheusPort = getFreePort(t)

	ctx, cancel := context.WithCancel(context.Background())
	shutdown := telemetry.Init(ctx, cfg, component)

	cleanup := func() {
		cancel()
		shutdown()
	}

	return cleanup, fmt.Sprintf("http://127.0.0.1:%d/metrics", cfg.PrometheusPort)
}

func scrapeMetrics(t *testing.T, url string) *http.Response {
	t.Helper()

	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(2 * time.Second)
	var resp *http.Response
	var err error

	for time.Now().Before(deadline) {
		resp, err = client.Get(url)
		if err == nil {
			return resp
		}
		time.Sleep(10 * time.Millisecond)
	}

	assert.NilError(t, err)
	return resp
}

func getFreePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NilError(t, err)
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port
}
