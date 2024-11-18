package controller

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gadget-inc/fusion/internal/function"
)

type Client struct {
	host string
}

func NewClient(host string) *Client {
	return &Client{host: host}
}

func (c *Client) Assign(ctx context.Context, fn function.Function) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+c.host+":8080/assign", nil)
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
