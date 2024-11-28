package fixture

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/google/uuid"
)

type EchoFixture struct {
	*Fixture
	Name     string
	Function function.Function
}

func NewEcho(t *testing.T, name string) *EchoFixture {
	return &EchoFixture{
		Fixture: New(t),
		Name:    name,
		Function: function.Function{
			Deployment:                 "echo",
			MaxReplicas:                1,
			MaxReplicasStr:             "1",
			Metadata:                   uuid.NewString(),
			MinReplicas:                0,
			MinReplicasStr:             "0",
			Namespace:                  "fusion-fixtures-test",
			TargetCPUUtilization:       100,
			TargetCPUUtilizationStr:    "100",
			TargetMemoryUtilization:    200,
			TargetMemoryUtilizationStr: "200",
			Tenant:                     name + "-" + uuid.NewString(),
		},
	}
}

func (f *EchoFixture) NewFunctionRequest(ctx context.Context, method, path string, body io.Reader) *http.Request {
	req := f.NewRouterRequest(ctx, method, path, body)
	f.Function.SetHeaders(req)
	return req
}

func (f *EchoFixture) SendFunctionRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req := f.NewFunctionRequest(ctx, method, path, body)
	return http.DefaultClient.Do(req)
}

type EchoResponse struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	header  http.Header
}

func (er *EchoResponse) Header() http.Header {
	if er.header == nil {
		er.header = make(http.Header)
		for k, v := range er.Headers {
			values := strings.Split(v, ",")
			for _, value := range values {
				er.header.Add(k, strings.TrimLeft(value, " "))
			}
		}
	}
	return er.header
}

func (f *EchoFixture) ParseFunctionResponse(res *http.Response) (EchoResponse, error) {
	var response EchoResponse
	err := json.NewDecoder(res.Body).Decode(&response)
	return response, err
}
