package fixture

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/function"
	"github.com/google/uuid"
)

const (
	FunctionNamespace  = "skipper-test-fixtures"
	FunctionDeployment = "test"
)

func NewFunction(t *testing.T) *function.Function {
	t.Helper()
	return &function.Function{
		Tenant:     "tenant-" + uuid.NewString()[:8],
		Metadata:   uuid.NewString(),
		Namespace:  FunctionNamespace,
		Deployment: FunctionDeployment,
		Scale: &function.Scale{
			MinInstances:           0,
			MaxInstances:           5,
			TargetCPUUsageMilli:    100,
			TargetMemoryUsageMiB:   200,
			TargetInFlightRequests: 50,
		},
	}
}

func NewFunctionRequest(t *testing.T, fn *function.Function, method string, path string, body io.Reader) *http.Request {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	t.Cleanup(cancel)
	req := httptest.NewRequestWithContext(ctx, method, path, body)
	fn.SetHeader(req)
	return req
}

func NewInstance(t *testing.T, fn *function.Function, handler http.HandlerFunc) *function.Instance {
	t.Helper()
	testServer := httptest.NewServer(handler)
	t.Cleanup(testServer.Close)

	return &function.Instance{
		Function:   fn,
		Name:       uuid.NewString(),
		Addr:       testServer.Listener.Addr().String(),
		ReplicaSet: CurrentReplicaSetName(fn),
		AssignedAt: time.Now(),
		ReadyAt:    time.Now(),
	}
}
