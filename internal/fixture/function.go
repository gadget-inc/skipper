package fixture

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	FunctionNamespace  = "skipper-test-fixtures"
	FunctionDeployment = "test"
)

func NewFunction(t *testing.T) *skipper.Function {
	t.Helper()
	return skipper.Function_builder{
		Tenant:     proto.String("tenant-" + uuid.NewString()[:8]),
		Metadata:   proto.String(uuid.NewString()),
		Namespace:  proto.String(FunctionNamespace),
		Deployment: proto.String(FunctionDeployment),
		Scale: skipper.Scale_builder{
			MinInstances:           proto.Uint32(0),
			MaxInstances:           proto.Uint32(5),
			TargetCpuUsageMilli:    proto.Uint32(100),
			TargetMemoryUsageMib:   proto.Uint32(200),
			TargetInFlightRequests: proto.Uint32(50),
		}.Build(),
	}.Build()
}

func NewFunctionRequest(t *testing.T, fn *skipper.Function, method string, path string, body io.Reader) *http.Request {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	t.Cleanup(cancel)
	req := httptest.NewRequestWithContext(ctx, method, path, body)
	fn.SetHeader(req)
	return req
}

func NewInstance(t *testing.T, fn *skipper.Function, handler http.HandlerFunc) *skipper.Instance {
	t.Helper()
	testServer := httptest.NewServer(handler)
	t.Cleanup(testServer.Close)

	return skipper.Instance_builder{
		Function:   fn,
		Name:       proto.String(uuid.NewString()),
		Addr:       proto.String(testServer.Listener.Addr().String()),
		ReplicaSet: proto.String(CurrentReplicaSetName(fn)),
		AssignedAt: timestamppb.Now(),
		ReadyAt:    timestamppb.Now(),
	}.Build()
}
