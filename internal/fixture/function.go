package fixture

import (
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
