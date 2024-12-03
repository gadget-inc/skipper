package controller

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	"github.com/gadget-inc/fusion/internal/function"
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

func (c *Client) Assign(ctx context.Context, fn function.Function) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.addr+"/assign", nil)
	if err != nil {
		return fmt.Errorf("failed to create assign request: %w", err)
	}

	fn.SetHeaders(req)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send assign request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("assign request failed: %s", res.Status)
	}

	return nil
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
