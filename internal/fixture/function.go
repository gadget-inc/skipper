package fixture

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/function"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	FunctionNamespace  = "skipper-test-fixtures"
	FunctionDeployment = "test"
)

func NewFunction() *function.Function {
	tenantCounter.Add(1)

	fn := new(function.Function)
	fn.SetTenant("tenant-" + strconv.Itoa(int(tenantCounter.Load())))
	fn.SetMetadata(uuid.NewString())
	fn.SetNamespace(FunctionNamespace)
	fn.SetDeployment(FunctionDeployment)

	scale := &function.Scale{}
	scale.SetMinInstances(0)
	scale.SetMaxInstances(5)
	scale.SetTargetCpuUsageMilli(100)
	scale.SetTargetMemoryUsageMib(200)
	scale.SetTargetInFlightRequests(50)
	fn.SetScale(scale)

	return fn
}

func NewFunctionRequest(t *testing.T, fn *function.Function, method string, path string, body io.Reader) *http.Request {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	req := httptest.NewRequestWithContext(ctx, method, path, body)
	fn.SetHeader(req)
	return req
}

func NewInstance(t *testing.T, fn *function.Function, handler http.HandlerFunc) *function.Instance {
	testServer := httptest.NewServer(handler)
	t.Cleanup(testServer.Close)

	instance := new(function.Instance)
	instance.SetFunction(fn)
	instance.SetName(uuid.NewString())
	instance.SetAddr(testServer.Listener.Addr().String())
	instance.SetReplicaSet(fn.GetDeployment() + "-replicaset-" + strconv.Itoa(int(replicaSetCounter.Load())))
	instance.SetAssignedAt(timestamppb.New(time.Now()))
	instance.SetReadyAt(timestamppb.New(time.Now()))
	return instance
}
