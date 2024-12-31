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
	DefaultFunctionNamespace = "fusion-fixtures-test"
	DefaultDeployment        = "test"
)

func NewFunction() function.Function {
	counterMu.Lock()
	defer counterMu.Unlock()

	tenantCounter++

	return function.Function{
		Tenant:                  "tenant-" + strconv.Itoa(tenantCounter),
		Metadata:                uuid.NewString(),
		Namespace:               DefaultFunctionNamespace,
		Deployment:              DefaultDeployment,
		MinInstances:            0,
		MaxInstances:            1,
		TargetCPUUtilization:    100,
		TargetMemoryUtilization: 200,
	}
}

func NewInstance(t *testing.T, fn function.Function, handler http.HandlerFunc) *function.Instance {
	testServer := httptest.NewServer(handler)
	t.Cleanup(testServer.Close)

	counterMu.Lock()
	defer counterMu.Unlock()

	return &function.Instance{
		Function:   fn,
		Name:       uuid.NewString(),
		Addr:       testServer.Listener.Addr().String(),
		Version:    fn.Deployment + "-replicaset-" + strconv.Itoa(replicaSetCounter),
		AssignedAt: time.Now(),
		ReadyAt:    time.Now(),
	}
}

func NewFunctionRequest(t *testing.T, fn function.Function, method string, path string, body io.Reader) *http.Request {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return NewFunctionRequestWithContext(t, ctx, fn, method, path, body)
}

func NewFunctionRequestWithContext(t *testing.T, ctx context.Context, fn function.Function, method string, path string, body io.Reader) *http.Request {
	req := httptest.NewRequestWithContext(ctx, method, path, body)
	fn.SetHeaders(req)
	return req
}
