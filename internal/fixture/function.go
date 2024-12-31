package fixture

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/google/uuid"
)

const (
	DefaultFunctionNamespace = "fusion-fixtures-test"
	DefaultDeployment        = "test"
)

type FunctionOption func(*function.Function)

func NewFunction(opts ...FunctionOption) function.Function {
	counterMu.Lock()
	defer counterMu.Unlock()

	tenantCounter++

	fn := function.Function{
		Tenant:                  "tenant-" + strconv.Itoa(tenantCounter),
		Metadata:                uuid.NewString(),
		Namespace:               DefaultFunctionNamespace,
		Deployment:              DefaultDeployment,
		MinInstances:            0,
		MaxInstances:            1,
		TargetCPUUtilization:    100,
		TargetMemoryUtilization: 200,
	}

	for _, opt := range opts {
		opt(&fn)
	}

	return fn
}

func WithMetadata(metadata string) FunctionOption {
	return func(fn *function.Function) {
		fn.Metadata = metadata
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
