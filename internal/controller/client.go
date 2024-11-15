package controller

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gadget-inc/fusion/internal/destination"
)

type Client struct {
	host string
}

func NewClient(host string) *Client {
	return &Client{host: host}
}

func (c *Client) Assign(ctx context.Context, dest destination.Destination) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+c.host+":8080", nil)
	if err != nil {
		return fmt.Errorf("failed to create assign request: %w", err)
	}

	dest.SetHeaders(req)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send assign request: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("assign request failed: %s", res.Status)
	}

	return nil
}
