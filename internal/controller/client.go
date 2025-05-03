package controller

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gadget-inc/skipper/internal/function"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/go-json-experiment/json"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type Client interface {
	Instance(ctx context.Context, fn *function.Function) (instance *function.Instance, err error)
	Heartbeat(ctx context.Context, routerIP string, heartbeats []*function.Heartbeat, forwardedFor ...string) error
	Scale(ctx context.Context, fn *function.Function, desiredInstances int, reason string) ([]*function.Instance, error)
}

type NewClientFunc func(host string, port int) Client

type httpClient struct {
	*http.Client
	addr string
}

var _ Client = &httpClient{}

func NewHTTPClient(host string, port int) Client {
	return &httpClient{
		addr: fmt.Sprintf("http://%s:%d", host, port),
		Client: &http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport,
				otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string { return "HTTP " + r.Method + " " + r.URL.Path }),
			),
		},
	}
}

func (c *httpClient) Instance(ctx context.Context, fn *function.Function) (instance *function.Instance, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.addr+"/instance", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create instance request: %w", err)
	}

	fn.SetHeader(req)

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

func (c *httpClient) Heartbeat(ctx context.Context, routerIP string, heartbeats []*function.Heartbeat, forwardedFor ...string) error {
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

func (c *httpClient) Scale(ctx context.Context, fn *function.Function, desiredInstances int, reason string) ([]*function.Instance, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.addr+"/scale", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create scale request: %w", err)
	}

	fn.SetHeader(req)
	req.Header[key.DesiredInstances.Header] = []string{strconv.Itoa(desiredInstances)}
	req.Header[key.Reason.Header] = []string{reason}

	res, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send scale request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scale request failed: status=%d body=%s", res.StatusCode, getResponseBody(res))
	}

	var instances []*function.Instance
	if err := json.UnmarshalRead(res.Body, &instances); err != nil {
		return nil, fmt.Errorf("failed to decode scale response: %w", err)
	}

	return instances, nil
}
