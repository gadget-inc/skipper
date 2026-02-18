package skipper

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/cespare/xxhash/v2"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/go-json-experiment/json"
)

// FunctionHash is a unique identifier for a Function, suitable for use as a map key.
type FunctionHash = uint64

// Hash returns a hash of the function's identity fields (namespace, deployment,
// tenant, oneshot), suitable for use as a map key. Metadata and scale fields are
// excluded so that changes to those fields don't create a new function identity.
func (f *Function) Hash() FunctionHash {
	h := xxhash.New()
	_, _ = h.WriteString(f.GetNamespace())
	_, _ = h.Write([]byte{0})
	_, _ = h.WriteString(f.GetDeployment())
	_, _ = h.Write([]byte{0})
	_, _ = h.WriteString(f.GetTenant())
	_, _ = h.Write([]byte{0})
	if f.GetOneshot() {
		_, _ = h.Write([]byte{1})
	} else {
		_, _ = h.Write([]byte{0})
	}
	return h.Sum64()
}

var _ slog.LogValuer = (*Function)(nil)

func (f *Function) LogValue() slog.Value {
	return slog.GroupValue(
		key.Namespace.Slog(f.GetNamespace()),
		key.Deployment.Slog(f.GetDeployment()),
		key.Tenant.Slog(f.GetTenant()),
		key.Metadata.Slog(f.GetMetadata()),
		key.Oneshot.Slog(f.GetOneshot()),
		key.Scale.Slog(f.GetScale()),
	)
}

func (f *Function) Validate() error {
	if f.GetNamespace() == "" {
		return errors.New("missing namespace")
	}
	if f.GetDeployment() == "" {
		return errors.New("missing deployment")
	}
	if f.GetTenant() == "" {
		return errors.New("missing tenant")
	}
	if f.GetScale() == nil {
		return errors.New("missing scale")
	}
	return nil
}

func (f *Function) SetHeader(r *http.Request) {
	fnJSON, err := json.Marshal(f)
	if err != nil {
		// this should never happen
		panic(fmt.Errorf("failed to marshal function: %w", err))
	}
	r.Header[key.Function.Header] = []string{string(fnJSON)}
}

func FunctionFromHeader(req *http.Request) (*Function, error) {
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
