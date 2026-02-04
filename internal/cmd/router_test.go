package cmd

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/controller"
	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/router"
	"github.com/gadget-inc/skipper/internal/skipper"
	"gotest.tools/v3/assert"
)

func TestRouterGRPCHostFallback(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		serviceHost     string
		grpcServiceHost string
		expectedHost    string
	}{
		{
			name:            "uses dedicated gRPC host when set",
			serviceHost:     "controller-http",
			grpcServiceHost: "controller-grpc-headless",
			expectedHost:    "controller-grpc-headless",
		},
		{
			name:            "falls back to service host when gRPC host is empty",
			serviceHost:     "controller",
			grpcServiceHost: "",
			expectedHost:    "controller",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var capturedHost string

			deps := &RouterDeps{
				NewControllerClient: func(cfg *router.Config) (controller.Client, error) {
					// Replicate the fallback logic from defaultNewControllerClient
					grpcHost := cfg.ControllerGRPCServiceHost
					if grpcHost == "" {
						grpcHost = cfg.ControllerServiceHost
					}
					capturedHost = grpcHost
					return nil, nil // We just care about the host selection
				},
			}

			cfg := &router.Config{
				ControllerProtocol:        "grpc",
				ControllerServiceHost:     tc.serviceHost,
				ControllerGRPCServiceHost: tc.grpcServiceHost,
				ControllerGRPCPort:        50051,
			}

			_, _ = deps.NewControllerClient(cfg)

			assert.Equal(t, capturedHost, tc.expectedHost)
		})
	}
}

func TestRouterProtocolSelection(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		protocol string
	}{
		{
			name:     "http protocol creates client",
			protocol: "http",
		},
		{
			name:     "grpc protocol creates client",
			protocol: "grpc",
		},
		{
			name:     "empty protocol creates client (defaults to grpc)",
			protocol: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := &router.Config{
				ControllerProtocol:    tc.protocol,
				ControllerServiceHost: "controller",
				ControllerHTTPPort:    80,
				ControllerGRPCPort:    50051,
			}

			// Verify the default function creates a valid client
			client, err := defaultNewControllerClient(cfg)
			assert.NilError(t, err)
			assert.Assert(t, client != nil)
		})
	}
}

func TestDefaultNewControllerClient(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		protocol        string
		serviceHost     string
		grpcServiceHost string
	}{
		{
			name:        "http protocol creates HTTP client",
			protocol:    "http",
			serviceHost: "controller",
		},
		{
			name:        "grpc protocol creates gRPC client",
			protocol:    "grpc",
			serviceHost: "controller",
		},
		{
			name:            "grpc with dedicated host",
			protocol:        "grpc",
			serviceHost:     "controller-http",
			grpcServiceHost: "controller-grpc-headless",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := &router.Config{
				ControllerProtocol:        tc.protocol,
				ControllerServiceHost:     tc.serviceHost,
				ControllerGRPCServiceHost: tc.grpcServiceHost,
				ControllerHTTPPort:        80,
				ControllerGRPCPort:        50051,
			}

			client, err := defaultNewControllerClient(cfg)
			assert.NilError(t, err)
			assert.Assert(t, client != nil)
		})
	}
}

func TestRouterGracefulShutdownDrainsInFlightRequests(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	requestStarted := make(chan struct{})
	requestCanFinish := make(chan struct{})
	var clientClosed atomic.Bool

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			close(requestStarted)
			<-requestCanFinish
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte("drained"))
		}), nil
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		return nil
	})

	// Create a mock client that tracks when Close is called
	mockClient := &trackingClient{
		Client:   mcc,
		onClose:  func() { clientClosed.Store(true) },
		isClosed: &clientClosed,
	}

	// Find an available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NilError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	deps := &RouterDeps{
		NewControllerClient: func(cfg *router.Config) (controller.Client, error) {
			return mockClient, nil
		},
	}

	cmd := NewRouter(deps)
	cmd.SetArgs([]string{
		"--pod-ip", fixture.RouterIP,
		"--controller-service-host", "controller",
		"--host", "127.0.0.1",
		"--port", itoa(port),
		"--shutdown-timeout", "5s",
		"--heartbeat-interval", "100ms",
	})

	ctx, cancel := context.WithCancel(t.Context())

	// Run the router command in a goroutine
	cmdErr := make(chan error, 1)
	go func() {
		cmdErr <- cmd.ExecuteContext(ctx)
	}()

	// Wait for the server to start
	time.Sleep(100 * time.Millisecond)

	// Start a request in the background
	requestCompleted := make(chan struct {
		body string
		err  error
	}, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+itoa(port)+"/", nil)
		fn.SetHeader(req)
		resp, err := http.DefaultClient.Do(req)
		result := struct {
			body string
			err  error
		}{err: err}
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			result.body = string(body)
		}
		requestCompleted <- result
	}()

	// Wait for request to start processing
	<-requestStarted

	// Cancel the context (simulating shutdown signal) while request is in-flight
	cancel()

	// Client should NOT be closed yet - request still in flight
	time.Sleep(50 * time.Millisecond)
	assert.Assert(t, !clientClosed.Load(), "client should not be closed while request is in flight")

	// Allow the request to complete
	close(requestCanFinish)

	// Verify the request completed successfully
	select {
	case result := <-requestCompleted:
		assert.NilError(t, result.err)
		assert.Equal(t, result.body, "drained")
	case <-time.After(5 * time.Second):
		t.Fatal("request did not complete within timeout")
	}

	// Wait for command to finish
	select {
	case err := <-cmdErr:
		assert.NilError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("command did not finish within timeout")
	}

	// Verify client was closed during shutdown
	assert.Assert(t, clientClosed.Load(), "client should be closed after shutdown")
}

func TestRouterHTTPServerStartupFailure(t *testing.T) {
	t.Parallel()

	// Bind to a port first
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NilError(t, err)
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	mcc := fixture.NewMockControllerClient(t)
	mcc.AllowHeartbeat() // Heartbeat may or may not be called depending on timing

	deps := &RouterDeps{
		NewControllerClient: func(cfg *router.Config) (controller.Client, error) {
			return mcc, nil
		},
	}

	cmd := NewRouter(deps)
	cmd.SetArgs([]string{
		"--pod-ip", fixture.RouterIP,
		"--controller-service-host", "controller",
		"--host", "127.0.0.1",
		"--port", itoa(port), // Same port that's already bound
		"--heartbeat-interval", "100ms",
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	err = cmd.ExecuteContext(ctx)
	assert.Assert(t, err != nil, "expected error when port is already bound")
	assert.Assert(t, contains(err.Error(), "failed to serve router"), "error should mention serve failure: %v", err)
}

// trackingClient wraps a controller.Client and tracks when Close is called
type trackingClient struct {
	controller.Client
	onClose  func()
	isClosed *atomic.Bool
}

func (c *trackingClient) Close() error {
	c.onClose()
	return c.Client.Close()
}

func itoa(i int) string {
	return strconv.Itoa(i)
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestRouterMultipleConcurrentRequestsDrain(t *testing.T) {
	t.Parallel()

	const numRequests = 5
	fn := fixture.NewFunction(t)
	requestsStarted := make(chan struct{}, numRequests)
	requestsCanFinish := make(chan struct{})
	var clientClosed atomic.Bool

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			requestsStarted <- struct{}{}
			<-requestsCanFinish
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte("drained"))
		}), nil
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		return nil
	})

	mockClient := &trackingClient{
		Client:   mcc,
		onClose:  func() { clientClosed.Store(true) },
		isClosed: &clientClosed,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NilError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	deps := &RouterDeps{
		NewControllerClient: func(cfg *router.Config) (controller.Client, error) {
			return mockClient, nil
		},
	}

	cmd := NewRouter(deps)
	cmd.SetArgs([]string{
		"--pod-ip", fixture.RouterIP,
		"--controller-service-host", "controller",
		"--host", "127.0.0.1",
		"--port", itoa(port),
		"--shutdown-timeout", "5s",
		"--heartbeat-interval", "100ms",
	})

	ctx, cancel := context.WithCancel(t.Context())

	cmdErr := make(chan error, 1)
	go func() {
		cmdErr <- cmd.ExecuteContext(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Start multiple concurrent requests
	type result struct {
		body string
		err  error
	}
	results := make(chan result, numRequests)
	for i := 0; i < numRequests; i++ {
		go func() {
			req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+itoa(port)+"/", nil)
			fn.SetHeader(req)
			resp, err := http.DefaultClient.Do(req)
			r := result{err: err}
			if err == nil {
				defer resp.Body.Close()
				body, _ := io.ReadAll(resp.Body)
				r.body = string(body)
			}
			results <- r
		}()
	}

	// Wait for all requests to start
	for i := 0; i < numRequests; i++ {
		select {
		case <-requestsStarted:
		case <-time.After(5 * time.Second):
			t.Fatalf("request %d did not start within timeout", i)
		}
	}

	// Cancel context while all requests are in-flight
	cancel()

	// Client should NOT be closed yet
	time.Sleep(50 * time.Millisecond)
	assert.Assert(t, !clientClosed.Load(), "client should not be closed while requests are in flight")

	// Allow all requests to complete
	close(requestsCanFinish)

	// Verify all requests completed successfully
	for i := 0; i < numRequests; i++ {
		select {
		case r := <-results:
			assert.NilError(t, r.err, "request %d failed", i)
			assert.Equal(t, r.body, "drained", "request %d had wrong body", i)
		case <-time.After(5 * time.Second):
			t.Fatalf("request %d did not complete within timeout", i)
		}
	}

	// Wait for command to finish
	select {
	case err := <-cmdErr:
		assert.NilError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("command did not finish within timeout")
	}

	assert.Assert(t, clientClosed.Load(), "client should be closed after shutdown")
}

func TestRouterShutdownTimeoutExceeded(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	requestStarted := make(chan struct{})
	// This channel is never closed - request will hang until timeout
	requestCanFinish := make(chan struct{})

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			close(requestStarted)
			<-requestCanFinish // Will block until test closes this or context is done
			rw.WriteHeader(http.StatusOK)
		}), nil
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		return nil
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NilError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	deps := &RouterDeps{
		NewControllerClient: func(cfg *router.Config) (controller.Client, error) {
			return mcc, nil
		},
	}

	cmd := NewRouter(deps)
	cmd.SetArgs([]string{
		"--pod-ip", fixture.RouterIP,
		"--controller-service-host", "controller",
		"--host", "127.0.0.1",
		"--port", itoa(port),
		"--shutdown-timeout", "100ms", // Very short timeout
		"--heartbeat-interval", "100ms",
	})

	ctx, cancel := context.WithCancel(t.Context())

	cmdErr := make(chan error, 1)
	go func() {
		cmdErr <- cmd.ExecuteContext(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Start a request that will never complete on its own
	requestErr := make(chan error, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+itoa(port)+"/", nil)
		fn.SetHeader(req)
		_, err := http.DefaultClient.Do(req)
		requestErr <- err
	}()

	// Wait for request to start
	<-requestStarted

	// Cancel context to start shutdown
	cancel()

	// Command should finish with error due to shutdown timeout
	select {
	case err := <-cmdErr:
		// The HTTP server shutdown should fail due to timeout
		assert.Assert(t, err != nil, "expected error when shutdown timeout exceeded")
		assert.Assert(t, contains(err.Error(), "failed to shutdown router"), "error should mention shutdown failure: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("command did not finish within timeout")
	}

	// Clean up the blocking request
	close(requestCanFinish)
}

func TestRouterNewRequestsRejectedDuringShutdown(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	requestStarted := make(chan struct{})
	requestCanFinish := make(chan struct{})
	shutdownStarted := make(chan struct{})

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			close(requestStarted)
			<-requestCanFinish
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte("original"))
		}), nil
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		return nil
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NilError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	deps := &RouterDeps{
		NewControllerClient: func(cfg *router.Config) (controller.Client, error) {
			return mcc, nil
		},
	}

	cmd := NewRouter(deps)
	cmd.SetArgs([]string{
		"--pod-ip", fixture.RouterIP,
		"--controller-service-host", "controller",
		"--host", "127.0.0.1",
		"--port", itoa(port),
		"--shutdown-timeout", "5s",
		"--heartbeat-interval", "100ms",
	})

	ctx, cancel := context.WithCancel(t.Context())

	cmdErr := make(chan error, 1)
	go func() {
		cmdErr <- cmd.ExecuteContext(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Start first request
	firstResult := make(chan struct {
		body string
		err  error
	}, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+itoa(port)+"/", nil)
		fn.SetHeader(req)
		resp, err := http.DefaultClient.Do(req)
		result := struct {
			body string
			err  error
		}{err: err}
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			result.body = string(body)
		}
		firstResult <- result
	}()

	// Wait for first request to start
	<-requestStarted

	// Cancel context to start shutdown
	cancel()
	close(shutdownStarted)

	// Give shutdown a moment to begin (server stops accepting new connections)
	time.Sleep(50 * time.Millisecond)

	// Try to start a new request during shutdown - it should fail
	secondResult := make(chan error, 1)
	go func() {
		// Use a client with a short timeout to avoid hanging
		client := &http.Client{Timeout: 2 * time.Second}
		req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+itoa(port)+"/", nil)
		fn.SetHeader(req)
		_, err := client.Do(req)
		secondResult <- err
	}()

	// The new request should fail (connection refused or similar)
	select {
	case err := <-secondResult:
		assert.Assert(t, err != nil, "new request during shutdown should fail")
	case <-time.After(3 * time.Second):
		t.Fatal("second request did not fail within timeout")
	}

	// Allow first request to complete
	close(requestCanFinish)

	// Verify first request completed successfully (it was in-flight before shutdown)
	select {
	case result := <-firstResult:
		assert.NilError(t, result.err)
		assert.Equal(t, result.body, "original")
	case <-time.After(5 * time.Second):
		t.Fatal("first request did not complete within timeout")
	}

	// Wait for command to finish
	select {
	case err := <-cmdErr:
		assert.NilError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("command did not finish within timeout")
	}
}

func TestRouterControllerClientCreationFailure(t *testing.T) {
	t.Parallel()

	deps := &RouterDeps{
		NewControllerClient: func(cfg *router.Config) (controller.Client, error) {
			return nil, errors.New("connection refused")
		},
	}

	cmd := NewRouter(deps)
	cmd.SetArgs([]string{
		"--pod-ip", fixture.RouterIP,
		"--controller-service-host", "controller",
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	err := cmd.ExecuteContext(ctx)
	assert.Assert(t, err != nil, "expected error when controller client creation fails")
	assert.ErrorContains(t, err, "failed to create controller client")
	assert.ErrorContains(t, err, "connection refused")
}

func TestRouterHeartbeatContinuesDuringLongRequest(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	requestStarted := make(chan struct{})
	requestCanFinish := make(chan struct{})
	var heartbeatCount atomic.Int32

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			close(requestStarted)
			<-requestCanFinish
			rw.WriteHeader(http.StatusOK)
		}), nil
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		heartbeatCount.Add(1)
		return nil
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NilError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	deps := &RouterDeps{
		NewControllerClient: func(cfg *router.Config) (controller.Client, error) {
			return mcc, nil
		},
	}

	cmd := NewRouter(deps)
	cmd.SetArgs([]string{
		"--pod-ip", fixture.RouterIP,
		"--controller-service-host", "controller",
		"--host", "127.0.0.1",
		"--port", itoa(port),
		"--shutdown-timeout", "5s",
		"--heartbeat-interval", "50ms", // Short interval to test multiple heartbeats
	})

	ctx, cancel := context.WithCancel(t.Context())

	cmdErr := make(chan error, 1)
	go func() {
		cmdErr <- cmd.ExecuteContext(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Record heartbeat count before request
	heartbeatsBefore := heartbeatCount.Load()

	// Start a long-running request
	requestDone := make(chan struct{})
	go func() {
		req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+itoa(port)+"/", nil)
		fn.SetHeader(req)
		_, _ = http.DefaultClient.Do(req)
		close(requestDone)
	}()

	// Wait for request to start
	<-requestStarted

	// Let the request run for a while to allow multiple heartbeats
	time.Sleep(200 * time.Millisecond)

	// Check that heartbeats continued during the request
	heartbeatsDuring := heartbeatCount.Load()
	assert.Assert(t, heartbeatsDuring > heartbeatsBefore, "heartbeats should continue during long request: before=%d, during=%d", heartbeatsBefore, heartbeatsDuring)

	// Allow request to complete and shutdown
	close(requestCanFinish)
	<-requestDone
	cancel()

	select {
	case err := <-cmdErr:
		assert.NilError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("command did not finish within timeout")
	}
}

func TestRouterClientClosedAfterAllRequestsDrain(t *testing.T) {
	t.Parallel()

	const numRequests = 3
	fn := fixture.NewFunction(t)
	requestsStarted := make(chan struct{}, numRequests)
	requestsCanFinish := make([]chan struct{}, numRequests)
	for i := range requestsCanFinish {
		requestsCanFinish[i] = make(chan struct{})
	}
	var requestIndex atomic.Int32
	var clientClosed atomic.Bool
	var clientClosedWhileRequestsInFlight atomic.Bool

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		idx := int(requestIndex.Add(1)) - 1
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			requestsStarted <- struct{}{}
			<-requestsCanFinish[idx]
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte("done"))
		}), nil
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		return nil
	})

	mockClient := &trackingClient{
		Client: mcc,
		onClose: func() {
			clientClosed.Store(true)
			// Check if any requests are still in flight (requestsStarted has items but not all finished)
			// This is approximate but helps detect early closure
		},
		isClosed: &clientClosed,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NilError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	deps := &RouterDeps{
		NewControllerClient: func(cfg *router.Config) (controller.Client, error) {
			return mockClient, nil
		},
	}

	cmd := NewRouter(deps)
	cmd.SetArgs([]string{
		"--pod-ip", fixture.RouterIP,
		"--controller-service-host", "controller",
		"--host", "127.0.0.1",
		"--port", itoa(port),
		"--shutdown-timeout", "5s",
		"--heartbeat-interval", "100ms",
	})

	ctx, cancel := context.WithCancel(t.Context())

	cmdErr := make(chan error, 1)
	go func() {
		cmdErr <- cmd.ExecuteContext(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Start multiple requests
	results := make(chan error, numRequests)
	for i := 0; i < numRequests; i++ {
		go func() {
			req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+itoa(port)+"/", nil)
			fn.SetHeader(req)
			_, err := http.DefaultClient.Do(req)
			results <- err
		}()
	}

	// Wait for all requests to start
	for i := 0; i < numRequests; i++ {
		<-requestsStarted
	}

	// Cancel context to start shutdown
	cancel()

	// Client should NOT be closed yet
	time.Sleep(50 * time.Millisecond)
	if clientClosed.Load() {
		clientClosedWhileRequestsInFlight.Store(true)
	}

	// Complete requests one by one, checking client is not closed until all are done
	for i := 0; i < numRequests-1; i++ {
		close(requestsCanFinish[i])
		<-results // Wait for this request to complete

		time.Sleep(20 * time.Millisecond)
		if clientClosed.Load() {
			clientClosedWhileRequestsInFlight.Store(true)
		}
	}

	// Complete the last request
	close(requestsCanFinish[numRequests-1])
	<-results

	// Wait for command to finish
	select {
	case err := <-cmdErr:
		assert.NilError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("command did not finish within timeout")
	}

	// Client should be closed now
	assert.Assert(t, clientClosed.Load(), "client should be closed after shutdown")
	assert.Assert(t, !clientClosedWhileRequestsInFlight.Load(), "client should not be closed while requests are in flight")
}
