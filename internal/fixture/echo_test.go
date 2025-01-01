package fixture

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEchoMethods(t *testing.T) {
	testCases := []struct {
		method string
	}{
		{http.MethodGet},
		{http.MethodPost},
		{http.MethodPut},
		{http.MethodPatch},
		{http.MethodDelete},
		{http.MethodOptions},
		{http.MethodTrace},
	}

	f := NewEcho(t, "methods")

	for _, tc := range testCases {
		t.Run(tc.method, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			res, err := f.SendFunctionRequest(ctx, tc.method, "/", nil)
			require.NoError(t, err, "failed to send function request")
			require.Equal(t, http.StatusOK, res.StatusCode, "unexpected status code")

			echoResponse, err := ParseEchoResponse(res)
			require.NoError(t, err, "failed to decode response")
			require.Equal(t, tc.method, echoResponse.Method, "unexpected method")
		})
	}
}

func TestEchoHeaders(t *testing.T) {
	testCases := []struct {
		name         string
		setHeaders   func(req *http.Request)
		checkHeaders func(t *testing.T, headers http.Header)
	}{
		{
			name:       "default",
			setHeaders: func(req *http.Request) {},
			checkHeaders: func(t *testing.T, headers http.Header) {
				// host, user-agent, accept-encoding, x-forwarded-for, x-forwarded-host, x-forwarded-proto
				require.Len(t, headers, 6, "unexpected number of headers")
			},
		},
		{
			name: "custom",
			setHeaders: func(req *http.Request) {
				req.Header.Set("X-Custom-Header", "custom-value")
				req.Header.Add("X-Custom-Multi-Header", "multi-value-1")
				req.Header.Add("X-Custom-Multi-Header", "multi-value-2")
			},
			checkHeaders: func(t *testing.T, headers http.Header) {
				require.Equal(t, "custom-value", headers.Get("X-Custom-Header"))
				require.Equal(t, []string{"multi-value-1", "multi-value-2"}, headers.Values("X-Custom-Multi-Header"))
				require.Len(t, headers, 8, "unexpected number of headers")
			},
		},
	}

	f := NewEcho(t, "headers")

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req := f.NewFunctionRequest(ctx, http.MethodGet, "/", nil)

			// override the Host and User-Agent headers
			req.Host = "echo.example.com"
			req.Header.Set("User-Agent", "echo-client")

			// set the test case headers
			tc.setHeaders(req)

			// send the request
			res, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, res.StatusCode)

			// parse the response
			echoResponse, err := ParseEchoResponse(res)
			echoResponseHeaders := echoResponse.Header()
			require.NoError(t, err)

			// verify the Host and User-Agent headers were received by the function
			require.Equal(t, "echo.example.com", echoResponseHeaders.Get("Host"))
			require.Equal(t, "echo-client", echoResponseHeaders.Get("User-Agent"))

			// verify the correct forwarded headers were received by the function
			require.NotEmpty(t, echoResponseHeaders.Get("X-Forwarded-For"))
			require.Equal(t, "echo.example.com", echoResponseHeaders.Get("X-Forwarded-Host"))
			require.Equal(t, "http", echoResponseHeaders.Get("X-Forwarded-Proto"))

			// the default go http client will add the Accept-Encoding header
			require.Equal(t, "gzip", echoResponseHeaders.Get("Accept-Encoding"))

			// verify the test case headers were received by the function
			tc.checkHeaders(t, echoResponseHeaders)
		})
	}
}

func TestEchoBody(t *testing.T) {
	testCases := []struct {
		name      string
		getBody   func() (string, io.Reader)
		checkBody func(t *testing.T, body string)
	}{
		{
			name: "empty",
			getBody: func() (string, io.Reader) {
				return "", nil
			},
			checkBody: func(t *testing.T, body string) {
				require.Empty(t, body)
			},
		},
		{
			name: "text",
			getBody: func() (string, io.Reader) {
				return "text/plain", strings.NewReader("hello, world!")
			},
			checkBody: func(t *testing.T, body string) {
				require.Equal(t, "hello, world!", body)
			},
		},
		{
			name: "json",
			getBody: func() (string, io.Reader) {
				return "application/json", strings.NewReader(`{"key":"value"}`)
			},
			checkBody: func(t *testing.T, body string) {
				require.Equal(t, `{"key":"value"}`, body)
			},
		},
	}

	f := NewEcho(t, "body")

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			contentEncoding, body := tc.getBody()
			req := f.NewFunctionRequest(ctx, http.MethodPost, "/", body)
			req.Header.Set("Content-Encoding", contentEncoding)

			res, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, res.StatusCode)

			echoResponse, err := ParseEchoResponse(res)
			require.NoError(t, err)

			require.Equal(t, contentEncoding, echoResponse.Header().Get("Content-Encoding"))
			tc.checkBody(t, echoResponse.Body)
		})
	}
}
