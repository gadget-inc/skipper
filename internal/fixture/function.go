package fixture

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/google/uuid"
)

const (
	FunctionNamespace  = "fusion-fixtures-test"
	FunctionDeployment = "test"
)

func NewFunction() function.Function {
	tenantCounter.Add(1)

	return function.Function{
		Tenant:     "tenant-" + strconv.Itoa(int(tenantCounter.Load())),
		Metadata:   uuid.NewString(),
		Namespace:  FunctionNamespace,
		Deployment: FunctionDeployment,
		Scale: function.Scale{
			MinInstances:         0,
			MaxInstances:         5,
			TargetCPUUsageMilli:  100,
			TargetMemoryUsageMiB: 200,
		},
	}
}

func NewFunctionRequest(t *testing.T, fn function.Function, method string, path string, body io.Reader) *http.Request {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	req := httptest.NewRequestWithContext(ctx, method, path, body)
	fn.SetHeader(req)
	return req
}
