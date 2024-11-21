package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gadget-inc/fusion/internal/function"
)

type Client struct {
	addr string
}

func NewClient() *Client {
	return &Client{
		addr: fmt.Sprintf("http://%s:%d", FlagHost.Value, FlagPort.Value),
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

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("assign request failed: %s", res.Status)
	}

	return nil
}

func (c *Client) Traffic(ctx context.Context, trafficEntries []TrafficEntry) error {
	if len(trafficEntries) == 0 {
		return nil
	}

	body, err := json.Marshal(trafficEntries)
	if err != nil {
		return fmt.Errorf("failed to marshal traffic: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.addr+"/traffic", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create traffic request: %w", err)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send traffic request: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("traffic request failed: %s", res.Status)
	}

	return nil
}
