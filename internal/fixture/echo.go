package fixture

import (
	"net/http"
	"strings"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/goccy/go-json"
)

func NewEchoFunction(opts ...FunctionOption) function.Function {
	return NewFunction(append(opts, WithDeployment("echo"), WithNamespace(DefaultFunctionNamespace))...)
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

func ParseEchoResponse(res *http.Response) (EchoResponse, error) {
	var response EchoResponse
	err := json.NewDecoder(res.Body).Decode(&response)
	return response, err
}
