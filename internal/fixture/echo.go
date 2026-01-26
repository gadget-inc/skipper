package fixture

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/go-json-experiment/json"
)

func NewEchoFunction(t *testing.T) *skipper.Function {
	t.Helper()
	fn := NewFunction(t)
	fn.SetDeployment("echo")
	return fn
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
			for value := range strings.SplitSeq(v, ",") {
				er.header.Add(k, strings.TrimLeft(value, " "))
			}
		}
	}
	return er.header
}

func ParseEchoResponse(res *http.Response) (EchoResponse, error) {
	var response EchoResponse
	err := json.UnmarshalRead(res.Body, &response)
	return response, err
}
