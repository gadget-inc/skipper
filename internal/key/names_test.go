package key

import (
	"testing"

	"gotest.tools/v3/assert"
)

func Test_newNames(t *testing.T) {
	t.Run("single word", func(t *testing.T) {
		n := newNames("namespace")

		assert.Equal(t, n.Name, "namespace")
		assert.Equal(t, n.Header, "X-Skipper-Namespace")
		assert.Equal(t, n.Label, "skipper/namespace")
		assert.Equal(t, n.PatchLabel, "/metadata/labels/skipper~1namespace")
		assert.Equal(t, n.PatchAnnotation, "/metadata/annotations/skipper~1namespace")
	})

	t.Run("underscores only", func(t *testing.T) {
		n := newNames("cpu_usage_milli")

		assert.Equal(t, n.Name, "cpu_usage_milli")
		assert.Equal(t, n.Header, "X-Skipper-Cpu-Usage-Milli")
		assert.Equal(t, n.Label, "skipper/cpu-usage-milli")
		assert.Equal(t, n.PatchLabel, "/metadata/labels/skipper~1cpu-usage-milli")
		assert.Equal(t, n.PatchAnnotation, "/metadata/annotations/skipper~1cpu-usage-milli")
	})

	t.Run("dot-snake OTEL style", func(t *testing.T) {
		n := newNames("http.response.status_code")

		assert.Equal(t, n.Name, "http.response.status_code")
		assert.Equal(t, n.Header, "X-Skipper-Http-Response-Status-Code")
		assert.Equal(t, n.Label, "skipper/http-response-status-code")
		assert.Equal(t, n.PatchLabel, "/metadata/labels/skipper~1http-response-status-code")
		assert.Equal(t, n.PatchAnnotation, "/metadata/annotations/skipper~1http-response-status-code")
	})
}
