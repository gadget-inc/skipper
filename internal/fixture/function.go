package fixture

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/google/uuid"
)

func NewFunction() function.Function {
	return function.Function{
		Tenant:                  "test-" + uuid.NewString(),
		Metadata:                uuid.NewString(),
		Namespace:               "fusion-fixtures-test",
		Deployment:              "test",
		MinInstances:            0,
		MaxInstances:            1,
		TargetCPUUtilization:    100,
		TargetMemoryUtilization: 200,
	}
}

func NewInstance(t *testing.T, fn function.Function, handler http.HandlerFunc) *function.Instance {
	testServer := httptest.NewServer(handler)
	t.Cleanup(testServer.Close)

	return &function.Instance{
		Function:   fn,
		Name:       uuid.NewString(),
		Addr:       testServer.Listener.Addr().String(),
		Version:    uuid.NewString(),
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
