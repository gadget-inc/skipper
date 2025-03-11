package function

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/goccy/go-json"
)

func RemoveHeader(r *http.Request) {
	delete(r.Header, key.Function.Header)
}

func (f Function) SetHeader(r *http.Request) {
	fnJSON, err := json.Marshal(f)
	if err != nil {
		// this should never happen
		panic(fmt.Errorf("failed to marshal function: %w", err))
	}
	r.Header[key.Function.Header] = []string{string(fnJSON)}
}

func FromHeader(req *http.Request) (Function, error) {
	var fn Function

	header, ok := req.Header[key.Function.Header]
	if !ok || len(header) == 0 {
		return fn, errors.New("missing " + key.Function.Header)
	}

	err := json.Unmarshal([]byte(header[0]), &fn)
	if err != nil {
		return fn, fmt.Errorf("failed to unmarshal %s header: %w", key.Function.Header, err)
	}

	if fn.Namespace == "" {
		return fn, errors.New("missing namespace")
	}
	if fn.Deployment == "" {
		return fn, errors.New("missing deployment")
	}
	if fn.Tenant == "" {
		return fn, errors.New("missing tenant")
	}
	if fn.Scale.MinInstances < 0 {
		return fn, errors.New("min instances must be greater than or equal to 0")
	}
	if fn.Scale.MaxInstances < 0 {
		return fn, errors.New("max instances must be greater than or equal to 0")
	}
	if fn.Scale.TargetCPUUsageMilli < 0 {
		return fn, errors.New("target cpu usage must be greater than or equal to 0")
	}
	if fn.Scale.TargetMemoryUsageMiB < 0 {
		return fn, errors.New("target memory usage must be greater than or equal to 0")
	}
	if fn.Scale.TargetInFlightRequests < 0 {
		return fn, errors.New("target in flight requests must be greater than or equal to 0")
	}

	return fn, nil
}
