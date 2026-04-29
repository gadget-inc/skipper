package fixture

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/go-json-experiment/json"
)

func NewFixtureFunction(t *testing.T) *skipper.Function {
	t.Helper()
	fn := NewFunction(t)
	fn.SetDeployment("fixture")
	return fn
}

type FixtureResponse struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	header  http.Header
}

func (er *FixtureResponse) Header() http.Header {
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

func ParseFixtureResponse(res *http.Response) (FixtureResponse, error) {
	var response FixtureResponse
	err := json.UnmarshalRead(res.Body, &response)
	return response, err
}
