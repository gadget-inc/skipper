package controller

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/goccy/go-json"
)

type Client struct {
	addr string
}

func NewClient(host string, port int) *Client {
	return &Client{
		addr: fmt.Sprintf("http://%s:%d", host, port),
	}
}

func (c *Client) Get(ctx context.Context, fn function.Function) (instance function.Instance, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.addr+"/get", nil)
	if err != nil {
		return instance, fmt.Errorf("failed to create get request: %w", err)
	}

	fn.SetHeaders(req)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return instance, fmt.Errorf("failed to send get request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return instance, fmt.Errorf("get request failed: %s", res.Status)
	}

	if err := json.NewDecoder(res.Body).Decode(&instance); err != nil {
		return instance, fmt.Errorf("failed to decode get response: %w", err)
	}

	return instance, nil
}

func (c *Client) KeepAlive(ctx context.Context, keepAlives []KeepAlive) error {
	if len(keepAlives) == 0 {
		return nil
	}

	body, err := json.Marshal(keepAlives)
	if err != nil {
		return fmt.Errorf("failed to marshal keep alives: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.addr+"/keepalive", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create keep alive request: %w", err)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send keep alive request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("keep alive request failed: %s", res.Status)
	}

	return nil
}

func (c *Client) Scale(ctx context.Context, fn function.Function, desiredInstances int) ([]function.Instance, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.addr+"/scale", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create scale request: %w", err)
	}

	fn.SetHeaders(req)
	req.Header[key.DesiredInstances.Header] = []string{strconv.Itoa(desiredInstances)}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send scale request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scale request failed: %s", res.Status)
	}

	var instances []function.Instance
	if err := json.NewDecoder(res.Body).Decode(&instances); err != nil {
		return nil, fmt.Errorf("failed to decode scale response: %w", err)
	}

	return instances, nil
}
