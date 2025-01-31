package function

import (
	"fmt"
	"net/http"

	"github.com/gadget-inc/fusion/internal/key"
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
		return fn, fmt.Errorf("missing %s header", key.Function.Header)
	}

	err := json.Unmarshal([]byte(header[0]), &fn)
	if err != nil {
		return fn, fmt.Errorf("failed to unmarshal function header: %w", err)
	}

	if fn.Namespace == "" {
		return fn, fmt.Errorf("missing function namespace")
	}
	if fn.Deployment == "" {
		return fn, fmt.Errorf("missing function deployment")
	}
	if fn.Tenant == "" {
		return fn, fmt.Errorf("missing function tenant")
	}
	if fn.MinInstances < 0 {
		return fn, fmt.Errorf("min instances must be greater than or equal to 0")
	}
	if fn.MaxInstances < 0 {
		return fn, fmt.Errorf("max instances must be greater than or equal to 0")
	}
	if fn.TargetCPUUtilization < 0 {
		return fn, fmt.Errorf("target CPU utilization must be greater than or equal to 0")
	}
	if fn.TargetMemoryUtilization < 0 {
		return fn, fmt.Errorf("target memory utilization must be greater than or equal to 0")
	}

	return fn, nil
}
