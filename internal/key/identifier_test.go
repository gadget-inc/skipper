package key

import (
	"testing"

	"gotest.tools/v3/assert"
)

func Test_newIdentifier(t *testing.T) {
	t.Run("single word", func(t *testing.T) {
		id := newIdentifier("namespace")

		assert.Equal(t, id.Name, "namespace")
		assert.Equal(t, id.Header, "X-Skipper-Namespace")
		assert.Equal(t, id.Label, "skipper/namespace")
		assert.Equal(t, id.Annotation, "skipper/namespace")
		assert.Equal(t, id.PatchLabel, "/metadata/labels/skipper~1namespace")
		assert.Equal(t, id.PatchAnnotation, "/metadata/annotations/skipper~1namespace")
	})

	t.Run("underscores only", func(t *testing.T) {
		id := newIdentifier("cpu_usage_milli")

		assert.Equal(t, id.Name, "cpu_usage_milli")
		assert.Equal(t, id.Header, "X-Skipper-Cpu-Usage-Milli")
		assert.Equal(t, id.Label, "skipper/cpu-usage-milli")
		assert.Equal(t, id.Annotation, "skipper/cpu-usage-milli")
		assert.Equal(t, id.PatchLabel, "/metadata/labels/skipper~1cpu-usage-milli")
		assert.Equal(t, id.PatchAnnotation, "/metadata/annotations/skipper~1cpu-usage-milli")
	})

	t.Run("dot-snake OTEL style", func(t *testing.T) {
		id := newIdentifier("http.response.status_code")

		assert.Equal(t, id.Name, "http.response.status_code")
		assert.Equal(t, id.Header, "X-Skipper-Http-Response-Status-Code")
		assert.Equal(t, id.Label, "skipper/http-response-status-code")
		assert.Equal(t, id.Annotation, "skipper/http-response-status-code")
		assert.Equal(t, id.PatchLabel, "/metadata/labels/skipper~1http-response-status-code")
		assert.Equal(t, id.PatchAnnotation, "/metadata/annotations/skipper~1http-response-status-code")
	})
}
