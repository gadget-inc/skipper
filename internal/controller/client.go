package controller

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/go-json-experiment/json"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type Client interface {
	Instance(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (instance *skipper.Instance, err error)
	Heartbeat(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error
	Scale(ctx context.Context, fn *skipper.Function, desiredInstances uint32, reason skipper.ScaleReason) ([]*skipper.Instance, error)
	ReplaceInstance(ctx context.Context, fn *skipper.Function, instanceName string) error
	Close() error
}

type NewClientFunc func(host string, port int) Client

type HTTPClient struct {
	*http.Client
	addr string
}

var _ Client = &HTTPClient{}

func NewHTTPClient(host string, port int) Client {
	return &HTTPClient{
		addr: fmt.Sprintf("http://%s:%d", host, port),
		Client: &http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport,
				otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string { return "HTTP " + r.Method + " " + r.URL.Path }),
			),
		},
	}
}

func (c *HTTPClient) Instance(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (instance *skipper.Instance, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.addr+"/instance", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create instance request: %w", err)
	}

	fn.SetHeader(req)
	if len(excludeInstanceNames) > 0 {
		req.Header[key.ExcludeInstanceNames.Header] = []string{strings.Join(excludeInstanceNames, ",")}
	}

	res, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send instance request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("instance request failed: status=%d body=%s", res.StatusCode, getResponseBody(res))
	}

	if err := json.UnmarshalRead(res.Body, &instance); err != nil {
		return nil, fmt.Errorf("failed to decode instance response: %w", err)
	}

	return instance, nil
}

func (c *HTTPClient) Heartbeat(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
	if len(heartbeats) == 0 {
		return nil
	}

	body, err := json.Marshal(heartbeats)
	if err != nil {
		return fmt.Errorf("failed to marshal heartbeats: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.addr+"/heartbeat", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create heartbeat request: %w", err)
	}

	req.Header[key.RouterIP.Header] = []string{routerIP}
	req.Header[key.ForwardedFor.Header] = append(req.Header[key.ForwardedFor.Header], forwardedFor...)

	res, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send heartbeat request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("heartbeat request failed: status=%d body=%s", res.StatusCode, getResponseBody(res))
	}

	return nil
}

func (c *HTTPClient) Scale(ctx context.Context, fn *skipper.Function, desiredInstances uint32, reason skipper.ScaleReason) ([]*skipper.Instance, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.addr+"/scale", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create scale request: %w", err)
	}

	fn.SetHeader(req)
	req.Header[key.DesiredInstances.Header] = []string{strconv.FormatUint(uint64(desiredInstances), 10)}
	req.Header[key.Reason.Header] = []string{reason.String()}

	res, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send scale request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scale request failed: status=%d body=%s", res.StatusCode, getResponseBody(res))
	}

	var instances []*skipper.Instance
	if err := json.UnmarshalRead(res.Body, &instances); err != nil {
		return nil, fmt.Errorf("failed to decode scale response: %w", err)
	}

	return instances, nil
}

func (c *HTTPClient) ReplaceInstance(ctx context.Context, fn *skipper.Function, instanceName string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.addr+"/replace-instance", nil)
	if err != nil {
		return fmt.Errorf("failed to create replace instance request: %w", err)
	}

	fn.SetHeader(req)
	req.Header[key.InstanceName.Header] = []string{instanceName}

	res, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send replace instance request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("replace instance request failed: status=%d body=%s", res.StatusCode, getResponseBody(res))
	}

	return nil
}

func (c *HTTPClient) Close() error {
	return nil
}
