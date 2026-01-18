package function

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/go-json-experiment/json"
)

func (f *Function) SetHeader(r *http.Request) {
	fnJSON, err := json.Marshal(f)
	if err != nil {
		// this should never happen
		panic(fmt.Errorf("failed to marshal function: %w", err))
	}
	r.Header[key.Function.Header] = []string{string(fnJSON)}
}

func FromHeader(req *http.Request) (*Function, error) {
	fn := &Function{}

	header, ok := req.Header[key.Function.Header]
	if !ok || len(header) == 0 {
		return nil, errors.New("missing " + key.Function.Header)
	}

	err := json.Unmarshal([]byte(header[0]), fn)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal %s header: %w", key.Function.Header, err)
	}

	if err := fn.Validate(); err != nil {
		return nil, err
	}

	return fn, nil
}
