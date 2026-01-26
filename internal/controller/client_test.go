package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/skipper"
	"gotest.tools/v3/assert"
)

func TestClientScaleReasonHeaderSerialization(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		reason         skipper.ScaleReason
		expectedHeader string
	}{
		{
			name:           "in flight requests",
			reason:         skipper.ScaleReason_SCALE_REASON_IN_FLIGHT_REQUESTS,
			expectedHeader: "SCALE_REASON_IN_FLIGHT_REQUESTS",
		},
		{
			name:           "cpu",
			reason:         skipper.ScaleReason_SCALE_REASON_CPU,
			expectedHeader: "SCALE_REASON_CPU",
		},
		{
			name:           "memory",
			reason:         skipper.ScaleReason_SCALE_REASON_MEMORY,
			expectedHeader: "SCALE_REASON_MEMORY",
		},
		{
			name:           "unspecified",
			reason:         skipper.ScaleReason_SCALE_REASON_UNSPECIFIED,
			expectedHeader: "SCALE_REASON_UNSPECIFIED",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var capturedReasonHeader string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedReasonHeader = r.Header.Get(key.Reason.Header)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("[]")) // return empty instances array
			}))
			defer server.Close()

			// Parse the server URL to get host and port
			client := NewHTTPClient(server.Listener.Addr().String(), 0)
			// Override the addr since NewHTTPClient formats it with http:// prefix
			client.(*httpClient).addr = server.URL

			fn := fixture.NewFunction(t)
			_, err := client.Scale(t.Context(), fn, 1, tc.reason)
			assert.NilError(t, err)

			assert.Equal(t, capturedReasonHeader, tc.expectedHeader,
				"ScaleReason header should be the enum name string, not a control character. Got %q (len=%d), expected %q",
				capturedReasonHeader, len(capturedReasonHeader), tc.expectedHeader)
		})
	}
}
