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

func NewFunctionRequest(t *testing.T, fn function.Function, method string, path string, body io.Reader) *http.Request {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	req := httptest.NewRequestWithContext(ctx, method, path, body)
	fn.SetHeader(req)
	return req
}

type FunctionOption func(*function.Function)

func WithNamespace(namespace string) FunctionOption {
	return func(fn *function.Function) {
		fn.Namespace = namespace
	}
}

func WithMetadata(metadata string) FunctionOption {
	return func(fn *function.Function) {
		fn.Metadata = metadata
	}
}

func WithDeployment(deployment string) FunctionOption {
	return func(fn *function.Function) {
		fn.Deployment = deployment
	}
}
