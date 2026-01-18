package function

import (
	"net/http/httptest"
	"testing"

	"github.com/gadget-inc/skipper/internal/key"
	"gotest.tools/v3/assert"
)

func TestFromHeader(t *testing.T) {
	validFn := &Function{
		Namespace:  "test-ns",
		Deployment: "test-deploy",
		Tenant:     "test-tenant",
		Metadata:   "test-metadata",
		Scale: &Scale{
			MinInstances:           1,
			MaxInstances:           10,
			TargetCPUUsageMilli:    500,
			TargetMemoryUsageMiB:   256,
			TargetInFlightRequests: 100,
		},
	}

	tests := []struct {
		name        string
		setupHeader func(req *httptest.ResponseRecorder) *httptest.ResponseRecorder
		header      string
		wantErr     string
		wantFn      *Function
	}{
		{
			name:    "missing header",
			header:  "",
			wantErr: "missing " + key.Function.Header,
		},
		{
			name:    "invalid JSON",
			header:  "{invalid json}",
			wantErr: "failed to unmarshal " + key.Function.Header + " header:",
		},
		{
			name:    "missing namespace",
			header:  `{"deployment":"d","tenant":"t"}`,
			wantErr: "missing namespace",
		},
		{
			name:    "missing deployment",
			header:  `{"namespace":"n","tenant":"t"}`,
			wantErr: "missing deployment",
		},
		{
			name:    "missing tenant",
			header:  `{"namespace":"n","deployment":"d"}`,
			wantErr: "missing tenant",
		},
		{
			name:    "negative min instances",
			header:  `{"namespace":"n","deployment":"d","tenant":"t","scale":{"min_instances":-1}}`,
			wantErr: "cannot unmarshal JSON number -1 into Go uint64",
		},
		{
			name:    "negative max instances",
			header:  `{"namespace":"n","deployment":"d","tenant":"t","scale":{"max_instances":-1}}`,
			wantErr: "cannot unmarshal JSON number -1 into Go uint64",
		},
		{
			name:    "negative target cpu usage",
			header:  `{"namespace":"n","deployment":"d","tenant":"t","scale":{"target_cpu_usage_milli":-1}}`,
			wantErr: "cannot unmarshal JSON number -1 into Go uint64",
		},
		{
			name:    "negative target memory usage",
			header:  `{"namespace":"n","deployment":"d","tenant":"t","scale":{"target_memory_usage_mib":-1}}`,
			wantErr: "cannot unmarshal JSON number -1 into Go uint64",
		},
		{
			name:    "negative target in flight requests",
			header:  `{"namespace":"n","deployment":"d","tenant":"t","scale":{"target_in_flight_requests":-1}}`,
			wantErr: "cannot unmarshal JSON number -1 into Go uint64",
		},
		{
			name:    "nil scale",
			header:  `{"namespace":"n","deployment":"d","tenant":"t"}`,
			wantErr: "missing scale",
		},
		{
			name:   "valid function with scale",
			header: `{"namespace":"test-ns","deployment":"test-deploy","tenant":"test-tenant","metadata":"test-metadata","scale":{"min_instances":1,"max_instances":10,"target_cpu_usage_milli":500,"target_memory_usage_mib":256,"target_in_flight_requests":100}}`,
			wantFn: validFn,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tc.header != "" {
				req.Header.Set(key.Function.Header, tc.header)
			}

			fn, err := FromHeader(req)

			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				assert.Assert(t, fn == nil, "expected nil function on error")
			} else {
				assert.NilError(t, err)
				assert.Assert(t, fn.Equal(tc.wantFn), "function mismatch: got %+v, want %+v", fn, tc.wantFn)
			}
		})
	}
}
