package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/function"
	"github.com/gadget-inc/skipper/internal/key"
	"gotest.tools/v3/assert"
)

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rw := httptest.NewRecorder()

	router := New(testConfig(), fixture.NewMockControllerClient(t))
	router.ServeHTTP(rw, req)

	assert.Assert(t, rw.Code == http.StatusOK)
	assert.Assert(t, rw.Body.Len() == 0)
}

func TestGetInstanceDuration(t *testing.T) {
	fn := fixture.NewFunction()

	sentError := false

	mockControllerClient := fixture.NewMockControllerClient(t)
	mockControllerClient.HandleInstance(func(ctx context.Context, fn function.Function, excludeInstanceNames ...string) (*function.Instance, error) {
		if !sentError {
			time.Sleep(10 * time.Millisecond)
			sentError = true
			return nil, errors.New("arbitrary error")
		}

		time.Sleep(10 * time.Millisecond)
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			assert.Assert(t, http.MethodGet == req.Method)
			assert.Assert(t, req.URL.Path == "/")

			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte("Hello, " + fn.Tenant))
		}), nil
	})

	rw := httptest.NewRecorder()
	req := fixture.NewFunctionRequest(t, fn, http.MethodGet, "/", nil)

	router := New(testConfig(), mockControllerClient)
	router.ServeHTTP(rw, req)

	assert.Assert(t, rw.Code == http.StatusOK)
	assert.Assert(t, "Hello, "+fn.Tenant == rw.Body.String())
	assert.Assert(t, len(rw.Header()[key.GetInstanceDurationMs.Header]) == 1)

	duration, err := strconv.ParseInt(rw.Header()[key.GetInstanceDurationMs.Header][0], 10, 64)
	assert.NilError(t, err)
	assert.Assert(t, duration >= 20)
}

func TestMethods(t *testing.T) {
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

	for _, tc := range testCases {
		// unit tests
		t.Run(tc.method, func(t *testing.T) {
			fn := fixture.NewFunction()

			mcc := fixture.NewMockControllerClient(t)
			mcc.HandleInstance(func(ctx context.Context, fn function.Function, excludeInstanceNames ...string) (*function.Instance, error) {
				return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
					assert.Assert(t, tc.method == req.Method)
				}), nil
			})

			rw := httptest.NewRecorder()
			req := fixture.NewFunctionRequest(t, fn, tc.method, "/", nil)

			router := New(testConfig(), mcc)
			router.ServeHTTP(rw, req)

			assert.Assert(t, rw.Code == http.StatusOK)
		})

		// integration tests
		t.Run(tc.method+" integration", func(t *testing.T) {
			if testing.Short() {
				t.Skip("skipping integration test in short mode")
			}

			fn := fixture.NewEchoFunction()
			req := fixture.NewFunctionRequest(t, fn, tc.method, fixture.RouterIntegrationURL, nil)

			res, err := http.DefaultTransport.RoundTrip(req)
			assert.NilError(t, err)
			defer res.Body.Close()
			assert.Assert(t, res.StatusCode == http.StatusOK)

			echoResponse, err := fixture.ParseEchoResponse(res)
			assert.NilError(t, err)
			assert.Assert(t, echoResponse.Method == tc.method)
		})
	}
}

func TestHeaders(t *testing.T) {
	type testState struct {
		fn      function.Function
		req     *http.Request
		headers http.Header
	}

	testCases := []struct {
		name  string
		setup func(*testing.T, *testState)
		check func(*testing.T, *testState)
	}{
		{
			name: "smoke",
			setup: func(t *testing.T, state *testState) {
				state.req.Host = state.fn.Tenant + ".example.com"
			},
			check: func(t *testing.T, state *testState) {
				expectedHost := state.fn.Tenant + ".example.com"
				expectedProto := "http"

				assert.DeepEqual(t, state.headers.Values("Host"), []string{expectedHost})
				assert.Assert(t, len(state.headers.Values("X-Forwarded-For")) == 1) // TODO: check the actual value
				assert.DeepEqual(t, state.headers.Values("X-Forwarded-Host"), []string{expectedHost})
				assert.DeepEqual(t, state.headers.Values("X-Forwarded-Proto"), []string{expectedProto})
				assert.DeepEqual(t, state.headers.Values("Forwarded"), []string{fmt.Sprintf("for=%s;host=%s;proto=%s", state.headers.Get("X-Forwarded-For"), expectedHost, expectedProto)})
				assert.Assert(t, len(state.headers) == 5)
			},
		},
		{
			name: "custom",
			setup: func(t *testing.T, state *testState) {
				state.req.Header.Set("X-Custom-Header", "custom-value")
				state.req.Header.Add("X-Custom-Multi-Header", "multi-value-1")
				state.req.Header.Add("X-Custom-Multi-Header", "multi-value-2")
			},
			check: func(t *testing.T, state *testState) {
				assert.DeepEqual(t, state.headers.Values("X-Custom-Header"), []string{"custom-value"})
				assert.DeepEqual(t, state.headers.Values("X-Custom-Multi-Header"), []string{"multi-value-1", "multi-value-2"})
				assert.Assert(t, len(state.headers) == 7)
			},
		},
	}

	for _, tc := range testCases {
		// unit tests
		t.Run(tc.name, func(t *testing.T) {
			state := &testState{
				fn: fixture.NewFunction(),
			}
			state.req = fixture.NewFunctionRequest(t, state.fn, http.MethodGet, "/", nil)

			mcc := fixture.NewMockControllerClient(t)
			mcc.HandleInstance(func(ctx context.Context, fn function.Function, excludeInstanceNames ...string) (*function.Instance, error) {
				return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
					req.Header.Set("Host", req.Host) // go removes the Host header, so we manually set it back
					state.headers = req.Header
				}), nil
			})

			tc.setup(t, state)

			rw := httptest.NewRecorder()
			router := New(testConfig(), mcc)
			router.ServeHTTP(rw, state.req)

			assert.Assert(t, rw.Code == http.StatusOK)
			assert.Assert(t, rw.Body.Len() == 0)

			tc.check(t, state)
		})

		// integration tests
		t.Run(tc.name+" integration", func(t *testing.T) {
			if testing.Short() {
				t.Skip("skipping integration test in short mode")
			}

			state := &testState{
				fn: fixture.NewEchoFunction(),
			}
			state.req = fixture.NewFunctionRequest(t, state.fn, http.MethodGet, fixture.RouterIntegrationURL, nil)
			state.req.Header.Set("User-Agent", "") // disable the default User-Agent header

			tc.setup(t, state)

			transport := &http.Transport{DisableCompression: true} // disable the default "Accept-Encoding: gzip" header
			res, err := transport.RoundTrip(state.req)
			assert.NilError(t, err)
			defer res.Body.Close()
			assert.Assert(t, res.StatusCode == http.StatusOK)

			echoResponse, err := fixture.ParseEchoResponse(res)
			assert.NilError(t, err)

			state.headers = echoResponse.Header()
			state.headers.Del("Traceparent") // ignore the Traceparent header since it may or may not be present depending on the test environment

			tc.check(t, state)
		})
	}
}

func TestBody(t *testing.T) {
	type testState struct {
		fn           function.Function
		contentType  string
		body         io.Reader
		receivedBody string
	}

	testCases := []struct {
		name  string
		setup func(*testing.T, *testState)
		check func(*testing.T, *testState)
	}{
		{
			name: "empty",
			setup: func(t *testing.T, state *testState) {
				state.contentType = ""
				state.body = nil
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.receivedBody) == 0)
			},
		},
		{
			name: "text",
			setup: func(t *testing.T, state *testState) {
				state.contentType = "text/plain"
				state.body = strings.NewReader("hello, world!")
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.receivedBody == "hello, world!")
			},
		},
		{
			name: "json",
			setup: func(t *testing.T, state *testState) {
				state.contentType = "application/json"
				state.body = strings.NewReader(`{"key":"value"}`)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.receivedBody == `{"key":"value"}`)
			},
		},
	}

	for _, tc := range testCases {
		// unit tests
		t.Run(tc.name, func(t *testing.T) {
			state := &testState{
				fn: fixture.NewFunction(),
			}

			tc.setup(t, state)

			mcc := fixture.NewMockControllerClient(t)
			mcc.HandleInstance(func(ctx context.Context, fn function.Function, excludeInstanceNames ...string) (*function.Instance, error) {
				return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
					content, err := io.ReadAll(req.Body)
					assert.NilError(t, err)
					state.receivedBody = string(content)
				}), nil
			})

			rw := httptest.NewRecorder()
			req := fixture.NewFunctionRequest(t, state.fn, http.MethodPost, "/", state.body)
			req.Header.Set("Content-Type", state.contentType)

			router := New(testConfig(), mcc)
			router.ServeHTTP(rw, req)

			assert.Assert(t, rw.Code == http.StatusOK)

			tc.check(t, state)
		})

		// integration tests
		t.Run(tc.name+" integration", func(t *testing.T) {
			if testing.Short() {
				t.Skip("skipping integration test in short mode")
			}

			state := &testState{
				fn: fixture.NewEchoFunction(),
			}

			tc.setup(t, state)

			req := fixture.NewFunctionRequest(t, state.fn, http.MethodPost, fixture.RouterIntegrationURL, state.body)
			req.Header.Set("Content-Type", state.contentType)

			res, err := http.DefaultTransport.RoundTrip(req)
			assert.NilError(t, err)
			defer res.Body.Close()
			assert.Assert(t, res.StatusCode == http.StatusOK)

			echoResponse, err := fixture.ParseEchoResponse(res)
			assert.NilError(t, err)

			state.receivedBody = echoResponse.Body

			tc.check(t, state)
		})
	}
}

func TestHeartbeats(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	t.Cleanup(cancel)

	testStartTime := time.Now()
	fn := fixture.NewFunction()
	once := new(sync.Once)
	done := make(chan struct{})
	defer close(done)

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn function.Function, excludeInstanceNames ...string) (*function.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte("Hello, " + fn.Tenant))
			<-done
		}), nil
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []function.Heartbeat, forwardedFor ...string) error {
		if len(heartbeats) == 0 {
			// ignore the initial heartbeats
			return nil
		}

		assert.Assert(t, routerIP == fixture.RouterIP)
		assert.Assert(t, len(heartbeats) == 1)
		assert.Assert(t, len(forwardedFor) == 0)

		heartbeat := heartbeats[0]
		assert.Assert(t, heartbeat.Function == fn)
		assert.Assert(t, heartbeat.Timestamp.After(testStartTime))
		if heartbeat.InFlightRequests > 0 {
			once.Do(func() {
				done <- struct{}{}
			})
		}
		return nil
	})

	rw := httptest.NewRecorder()
	req := fixture.NewFunctionRequest(t, fn, http.MethodGet, "/", nil).WithContext(ctx)

	router := New(testConfig(), mcc)
	router.Start(ctx)
	router.ServeHTTP(rw, req)

	assert.Assert(t, rw.Code == http.StatusOK)
	assert.Assert(t, rw.Body.String() == "Hello, "+fn.Tenant)

	heartbeat, ok := router.heartbeats.Load(fn.Hash())
	assert.Assert(t, ok)
	assert.Assert(t, heartbeat.Function == fn)
	assert.Assert(t, heartbeat.Timestamp.After(testStartTime))
	assert.Assert(t, heartbeat.InFlightRequests == 0) // ensure the number of in-flight requests is 0 now that the request is complete
}

func TestRetries(t *testing.T) {
	type testState struct {
		fn            function.Function
		rw            *httptest.ResponseRecorder
		maxAttempts   int
		instanceErrs  []error
		roundTripErrs []error
	}

	testCases := []struct {
		name  string
		setup func(*testing.T, *testState)
		check func(*testing.T, *testState)
	}{
		{
			name: "no errors",
			setup: func(t *testing.T, state *testState) {
				state.maxAttempts = 1
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)
				assert.Assert(t, state.rw.Body.String() == "Hello, "+state.fn.Tenant)
			},
		},
		{
			name: "ctrl.Instance arbitrary error",
			setup: func(t *testing.T, state *testState) {
				state.maxAttempts = 2
				state.instanceErrs = []error{errors.New("arbitrary error")}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)
				assert.Assert(t, state.rw.Body.String() == "Hello, "+state.fn.Tenant)
			},
		},
		{
			name: "roundTripper dial error",
			setup: func(t *testing.T, state *testState) {
				state.maxAttempts = 2
				state.roundTripErrs = []error{&net.OpError{Op: "dial", Err: errors.New("arbitrary error")}}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)
				assert.Assert(t, state.rw.Body.String() == "Hello, "+state.fn.Tenant)
			},
		},
		{
			name: "ctrl.Instance and roundTripper errors",
			setup: func(t *testing.T, state *testState) {
				state.maxAttempts = 3
				state.instanceErrs = []error{errors.New("arbitrary error")}
				state.roundTripErrs = []error{&net.OpError{Op: "dial", Err: errors.New("arbitrary error")}}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)
				assert.Assert(t, state.rw.Body.String() == "Hello, "+state.fn.Tenant)
			},
		},
		{
			name: "ctrl.Instance and roundTripper errors exceed max attempts",
			setup: func(t *testing.T, state *testState) {
				state.maxAttempts = 2
				state.instanceErrs = []error{errors.New("arbitrary error")}
				state.roundTripErrs = []error{&net.OpError{Op: "dial", Err: errors.New("arbitrary error")}}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusBadGateway)
				assert.Assert(t, state.rw.Body.Len() == 0)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			state := &testState{
				fn: fixture.NewFunction(),
				rw: httptest.NewRecorder(),
			}

			tc.setup(t, state)

			expectedMethod := http.MethodPost
			expectedPath := "/"
			expectedBody := "Hello, world!"

			instanceErrsIndex := 0
			mcc := fixture.NewMockControllerClient(t)
			mcc.HandleInstance(func(ctx context.Context, fn function.Function, excludeInstanceNames ...string) (*function.Instance, error) {
				if len(state.instanceErrs) > 0 && instanceErrsIndex < len(state.instanceErrs) {
					instanceErrsIndex++
					return nil, state.instanceErrs[instanceErrsIndex-1]
				}

				return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
					assert.Assert(t, req.Method == expectedMethod)
					assert.Assert(t, req.URL.Path == expectedPath)

					receivedBody, err := io.ReadAll(req.Body)
					assert.NilError(t, err)
					assert.Assert(t, string(receivedBody) == expectedBody)

					rw.WriteHeader(http.StatusOK)
					rw.Write([]byte("Hello, " + fn.Tenant))
				}), nil
			})

			body := &noReadAfterClose{ReadCloser: io.NopCloser(strings.NewReader(expectedBody))}
			req := fixture.NewFunctionRequest(t, state.fn, expectedMethod, expectedPath, body)

			cfg := testConfig()
			cfg.MaxRoundTripAttempts = state.maxAttempts
			router := New(cfg, mcc)

			roundTripperErrsIndex := 0
			originalTransport := router.roundTripper
			router.roundTripper = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				defer req.Body.Close() // RoundTripper always closes the body so imitate that behavior
				if len(state.roundTripErrs) > 0 && roundTripperErrsIndex < len(state.roundTripErrs) {
					roundTripperErrsIndex++
					return nil, state.roundTripErrs[roundTripperErrsIndex-1]
				}
				return originalTransport.RoundTrip(req)
			})

			router.ServeHTTP(state.rw, req)

			tc.check(t, state)
			assert.Assert(t, body.closed)
		})
	}
}

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// noReadAfterClose wraps an io.ReadCloser and returns
// http.ErrBodyReadAfterClose after Close is called.
//
// This is used to mimic net/http's internal body implementation:
// https://cs.opensource.google/go/go/+/refs/tags/go1.25.4:src/net/http/transfer.go;l=825-838;drc=998ce1c4262aab0153b5e89f84ef2ddd57507ec7
type noReadAfterClose struct {
	io.ReadCloser
	closed bool
}

func (r *noReadAfterClose) Read(p []byte) (n int, err error) {
	if r.closed {
		return 0, http.ErrBodyReadAfterClose
	}
	return r.ReadCloser.Read(p)
}

func (r *noReadAfterClose) Close() error {
	r.closed = true
	return r.ReadCloser.Close()
}
