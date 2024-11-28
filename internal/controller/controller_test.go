package controller_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gadget-inc/fusion/internal/test"
	"github.com/stretchr/testify/assert"
)

func TestController(t *testing.T) {
	fixture := test.NewFixture(t, "test-controller")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost:31021/", bytes.NewBufferString("hello world!"))
	assert.NoError(t, err, "failed to create request")

	req.Header.Set("Content-Type", "text/plain")
	fixture.Function.SetHeaders(req)

	res, err := http.DefaultClient.Do(req)
	assert.NoError(t, err, "failed to send request")
	assert.Equal(t, http.StatusOK, res.StatusCode, "unexpected status code")

	var response struct {
		Method  string            `json:"method"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}

	err = json.NewDecoder(res.Body).Decode(&response)
	assert.NoError(t, err, "failed to decode response")
	assert.Equal(t, http.MethodPost, response.Method, "unexpected method")
	assert.Equal(t, "http://localhost:31021/", response.URL, "unexpected url")
	assert.Equal(t, "hello world!", response.Body, "unexpected body")
	assert.Equal(t, "text/plain", response.Headers["content-type"], "unexpected content type")
}
