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

	"github.com/gadget-inc/skipper/internal/controller"
	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/telemetry"
	"github.com/prometheus/common/expfmt"
	"github.com/shoenig/test/must"
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

	must.Eq(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	must.NoError(t, err)
	must.True(t, len(body) > 0)

	var parser expfmt.TextParser
	metrics, err := parser.TextToMetricFamilies(bytes.NewReader(body))
	must.NoError(t, err)
	must.True(t, len(metrics) > 0)

	must.NotNil(t, metrics["skipper_controller_informer_events_total"])
	must.NotNil(t, metrics["skipper_controller_informer_event_lag_seconds"])
}

func TestPrometheusMetricsEndpoints_Router(t *testing.T) {
	cleanup, metricsURL := bootMetricsServer(t, "router")
	t.Cleanup(cleanup)

	resp := scrapeMetrics(t, metricsURL)
	defer resp.Body.Close()

	must.Eq(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	must.NoError(t, err)
	must.True(t, len(body) > 0)

	var parser expfmt.TextParser
	metrics, err := parser.TextToMetricFamilies(bytes.NewReader(body))
	must.NoError(t, err)
	must.True(t, len(metrics) > 0)
}

func bootMetricsServer(t *testing.T, component string) (func(), string) {
	t.Helper()

	fixture.SetFlag(t, &telemetry.FlagTelemetry, true)
	fixture.SetFlag(t, &telemetry.FlagTelemetryMetric, true)
	fixture.SetFlag(t, &telemetry.FlagTelemetryTrace, false)
	fixture.SetFlag(t, &telemetry.FlagTelemetryShutdownTimeout, 2*time.Second)
	fixture.SetFlag(t, &telemetry.FlagTelemetryPrometheusHost, "127.0.0.1")
	fixture.SetFlag(t, &telemetry.FlagTelemetryMetricOTLP, false)

	port := getFreePort(t)
	fixture.SetFlag(t, &telemetry.FlagTelemetryPrometheusPort, port)

	ctx, cancel := context.WithCancel(context.Background())
	shutdown := telemetry.Init(ctx, component)

	cleanup := func() {
		cancel()
		shutdown()
	}

	return cleanup, fmt.Sprintf("http://127.0.0.1:%d/metrics", port)
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

	must.NoError(t, err)
	return resp
}

func getFreePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	must.NoError(t, err)
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port
}
