package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/protobuf/proto"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/poll"
)

func TestHealthz(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rw := httptest.NewRecorder()

	router := New(testConfig(), fixture.NewMockControllerClient(t))
	router.ServeHTTP(rw, req)

	assert.Assert(t, rw.Code == http.StatusOK)
	assert.Assert(t, rw.Body.Len() == 0)
}

func TestBadRequest(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		setupHeader   func(*http.Request)
		expectedError string
	}{
		{
			name:          "missing function header",
			setupHeader:   func(req *http.Request) {},
			expectedError: "missing " + skipper.FunctionKey.Header,
		},
		{
			name: "malformed json",
			setupHeader: func(req *http.Request) {
				req.Header.Set(skipper.FunctionKey.Header, "not valid json")
			},
			expectedError: "failed to unmarshal " + skipper.FunctionKey.Header + " header",
		},
		{
			name: "missing namespace",
			setupHeader: func(req *http.Request) {
				req.Header.Set(skipper.FunctionKey.Header, `{"deployment":"test","tenant":"test","scale":{"minInstances":0,"maxInstances":5}}`)
			},
			expectedError: "missing namespace",
		},
		{
			name: "missing deployment",
			setupHeader: func(req *http.Request) {
				req.Header.Set(skipper.FunctionKey.Header, `{"namespace":"test","tenant":"test","scale":{"minInstances":0,"maxInstances":5}}`)
			},
			expectedError: "missing deployment",
		},
		{
			name: "missing tenant",
			setupHeader: func(req *http.Request) {
				req.Header.Set(skipper.FunctionKey.Header, `{"namespace":"test","deployment":"test","scale":{"minInstances":0,"maxInstances":5}}`)
			},
			expectedError: "missing tenant",
		},
		{
			name: "missing scale",
			setupHeader: func(req *http.Request) {
				req.Header.Set(skipper.FunctionKey.Header, `{"namespace":"test","deployment":"test","tenant":"test"}`)
			},
			expectedError: "missing scale",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			tc.setupHeader(req)

			rw := httptest.NewRecorder()
			router := New(testConfig(), fixture.NewMockControllerClient(t))
			router.ServeHTTP(rw, req)

			assert.Assert(t, rw.Code == http.StatusBadRequest)
			assert.Assert(t, strings.Contains(rw.Body.String(), tc.expectedError), "expected %q to contain %q", rw.Body.String(), tc.expectedError)
		})
	}
}

func TestGetInstanceDuration(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)

	sentError := false

	mockControllerClient := fixture.NewMockControllerClient(t)
	mockControllerClient.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
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
			rw.Write([]byte("Hello, " + fn.GetTenant()))
		}), nil
	})

	rw := httptest.NewRecorder()
	req := fixture.NewFunctionRequest(t, fn, http.MethodGet, "/", nil)

	router := New(testConfig(), mockControllerClient)
	router.ServeHTTP(rw, req)

	assert.Assert(t, rw.Code == http.StatusOK)
	assert.Assert(t, "Hello, "+fn.GetTenant() == rw.Body.String())
	assert.Assert(t, len(rw.Header()[key.GetInstanceDurationMs.Header]) == 1)

	duration, err := strconv.ParseInt(rw.Header()[key.GetInstanceDurationMs.Header][0], 10, 64)
	assert.NilError(t, err)
	assert.Assert(t, duration >= 20)
}

func TestMethods(t *testing.T) {
	t.Parallel()

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
			t.Parallel()
			fn := fixture.NewFunction(t)

			mcc := fixture.NewMockControllerClient(t)
			mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
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

			fn := fixture.NewFixtureFunction(t)
			req := fixture.NewFunctionRequest(t, fn, tc.method, fixture.RouterIntegrationURL(), nil)

			res, err := http.DefaultTransport.RoundTrip(req)
			assert.NilError(t, err)
			defer res.Body.Close()
			assert.Assert(t, res.StatusCode == http.StatusOK)

			fixtureResponse, err := fixture.ParseFixtureResponse(res)
			assert.NilError(t, err)
			assert.Assert(t, fixtureResponse.Method == tc.method)
		})
	}
}

func TestHeaders(t *testing.T) {
	t.Parallel()

	type testState struct {
		fn      *skipper.Function
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
				state.req.Host = state.fn.GetTenant() + ".example.com"
			},
			check: func(t *testing.T, state *testState) {
				expectedHost := state.fn.GetTenant() + ".example.com"
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
		{
			name: "x-forwarded-for preserved",
			setup: func(t *testing.T, state *testState) {
				state.req.Header.Set("X-Forwarded-For", "203.0.113.50")
			},
			check: func(t *testing.T, state *testState) {
				assert.DeepEqual(t, state.headers.Values("X-Forwarded-For"), []string{"203.0.113.50"})
			},
		},
		{
			name: "x-forwarded-host preserved",
			setup: func(t *testing.T, state *testState) {
				state.req.Header.Set("X-Forwarded-Host", "original-host.example.com")
			},
			check: func(t *testing.T, state *testState) {
				assert.DeepEqual(t, state.headers.Values("X-Forwarded-Host"), []string{"original-host.example.com"})
			},
		},
		{
			name: "x-forwarded-proto preserved",
			setup: func(t *testing.T, state *testState) {
				state.req.Header.Set("X-Forwarded-Proto", "https")
			},
			check: func(t *testing.T, state *testState) {
				assert.DeepEqual(t, state.headers.Values("X-Forwarded-Proto"), []string{"https"})
			},
		},
		{
			name: "forwarded preserved",
			setup: func(t *testing.T, state *testState) {
				state.req.Header.Set("Forwarded", "for=192.0.2.60;host=example.com;proto=https")
			},
			check: func(t *testing.T, state *testState) {
				assert.DeepEqual(t, state.headers.Values("Forwarded"), []string{"for=192.0.2.60;host=example.com;proto=https"})
			},
		},
		{
			name: "ipv6 in forwarded header",
			setup: func(t *testing.T, state *testState) {
				state.req.Host = "example.com"
				state.req.Header.Set("X-Forwarded-For", "2001:db8::1")
			},
			check: func(t *testing.T, state *testState) {
				assert.DeepEqual(t, state.headers.Values("X-Forwarded-For"), []string{"2001:db8::1"})
				// IPv6 addresses should be enclosed in brackets in the Forwarded header
				assert.Assert(t, strings.Contains(state.headers.Get("Forwarded"), `for="[2001:db8::1]"`))
			},
		},
	}

	for _, tc := range testCases {
		// unit tests
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			state := &testState{
				fn: fixture.NewFunction(t),
			}
			state.req = fixture.NewFunctionRequest(t, state.fn, http.MethodGet, "/", nil)

			mcc := fixture.NewMockControllerClient(t)
			mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
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
				fn: fixture.NewFixtureFunction(t),
			}
			state.req = fixture.NewFunctionRequest(t, state.fn, http.MethodGet, fixture.RouterIntegrationURL(), nil)
			state.req.Header.Set("User-Agent", "") // disable the default User-Agent header

			tc.setup(t, state)

			transport := &http.Transport{DisableCompression: true} // disable the default "Accept-Encoding: gzip" header
			res, err := transport.RoundTrip(state.req)
			assert.NilError(t, err)
			defer res.Body.Close()
			assert.Assert(t, res.StatusCode == http.StatusOK)

			fixtureResponse, err := fixture.ParseFixtureResponse(res)
			assert.NilError(t, err)

			state.headers = fixtureResponse.Header()
			state.headers.Del("Traceparent") // ignore the Traceparent header since it may or may not be present depending on the test environment

			tc.check(t, state)
		})
	}
}

func TestBody(t *testing.T) {
	t.Parallel()

	type testState struct {
		fn           *skipper.Function
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
			t.Parallel()

			state := &testState{
				fn: fixture.NewFunction(t),
			}

			tc.setup(t, state)

			mcc := fixture.NewMockControllerClient(t)
			mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
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
				fn: fixture.NewFixtureFunction(t),
			}

			tc.setup(t, state)

			req := fixture.NewFunctionRequest(t, state.fn, http.MethodPost, fixture.RouterIntegrationURL(), state.body)
			req.Header.Set("Content-Type", state.contentType)

			res, err := http.DefaultTransport.RoundTrip(req)
			assert.NilError(t, err)
			defer res.Body.Close()
			assert.Assert(t, res.StatusCode == http.StatusOK)

			fixtureResponse, err := fixture.ParseFixtureResponse(res)
			assert.NilError(t, err)

			state.receivedBody = fixtureResponse.Body

			tc.check(t, state)
		})
	}
}

func TestHeartbeats(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	testStartTime := time.Now()
	fn := fixture.NewFunction(t)
	once := new(sync.Once)
	done := make(chan struct{})
	defer close(done)

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte("Hello, " + fn.GetTenant()))
			<-done
		}), nil
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		if len(heartbeats) == 0 {
			// ignore the initial heartbeats
			return nil
		}

		assert.Assert(t, routerIP == fixture.RouterIP)
		assert.Assert(t, len(heartbeats) == 1)
		assert.Assert(t, len(forwardedFor) == 0)

		heartbeat := heartbeats[0]
		assert.Assert(t, proto.Equal(heartbeat.GetFunction(), fn))
		assert.Assert(t, heartbeat.GetTimestamp().AsTime().After(testStartTime))
		if heartbeat.GetInFlightRequests() > 0 {
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
	assert.Assert(t, rw.Body.String() == "Hello, "+fn.GetTenant())

	state, ok := router.heartbeats.Load(fn.Hash())
	assert.Assert(t, ok)
	assert.Assert(t, proto.Equal(state.fn.Load(), fn))
	assert.Assert(t, state.lastActiveTime().After(testStartTime))
	assert.Assert(t, state.inFlight.Load() == 0) // ensure the number of in-flight requests is 0 now that the request is complete
}

func TestRetries(t *testing.T) {
	t.Parallel()

	type testState struct {
		fn            *skipper.Function
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
				assert.Assert(t, state.rw.Body.String() == "Hello, "+state.fn.GetTenant())
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
				assert.Assert(t, state.rw.Body.String() == "Hello, "+state.fn.GetTenant())
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
				assert.Assert(t, state.rw.Body.String() == "Hello, "+state.fn.GetTenant())
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
				assert.Assert(t, state.rw.Body.String() == "Hello, "+state.fn.GetTenant())
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
			t.Parallel()

			state := &testState{
				fn: fixture.NewFunction(t),
				rw: httptest.NewRecorder(),
			}

			tc.setup(t, state)

			expectedMethod := http.MethodPost
			expectedPath := "/"
			expectedBody := "Hello, world!"

			instanceErrsIndex := 0
			mcc := fixture.NewMockControllerClient(t)
			mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
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
					rw.Write([]byte("Hello, " + fn.GetTenant()))
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

func TestInstanceExclusion(t *testing.T) {
	t.Parallel()

	type testState struct {
		fn                            *skipper.Function
		rw                            *httptest.ResponseRecorder
		router                        *Router
		mcc                           *fixture.MockControllerClient
		excludedInstanceNamesReceived [][]string
	}

	testCases := []struct {
		name  string
		setup func(*testing.T, *testState)
		check func(*testing.T, *testState)
	}{
		{
			name: "dial error excludes instance",
			setup: func(t *testing.T, state *testState) {
				failingInstance := skipper.Instance_builder{
					Function: state.fn,
					Name:     new("failing-instance"),
					Addr:     new("127.0.0.1:59999"), // non-existent address
				}.Build()
				successInstance := fixture.NewInstance(t, state.fn, func(rw http.ResponseWriter, req *http.Request) {
					rw.WriteHeader(http.StatusOK)
					rw.Write([]byte("success"))
				})

				callCount := 0
				state.mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
					state.excludedInstanceNamesReceived = append(state.excludedInstanceNamesReceived, excludeInstanceNames)
					callCount++
					if callCount == 1 {
						return failingInstance, nil
					}
					return successInstance, nil
				})
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)
				assert.Assert(t, state.rw.Body.String() == "success")
				assert.Assert(t, len(state.excludedInstanceNamesReceived) == 2)
				assert.Assert(t, len(state.excludedInstanceNamesReceived[0]) == 0)
				assert.DeepEqual(t, state.excludedInstanceNamesReceived[1], []string{"failing-instance"})
			},
		},
		{
			name: "non-dial error does not exclude instance",
			setup: func(t *testing.T, state *testState) {
				instance := fixture.NewInstance(t, state.fn, func(rw http.ResponseWriter, req *http.Request) {
					rw.WriteHeader(http.StatusOK)
					rw.Write([]byte("success"))
				})

				callCount := 0
				state.mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
					state.excludedInstanceNamesReceived = append(state.excludedInstanceNamesReceived, excludeInstanceNames)
					callCount++
					return instance, nil
				})

				originalTransport := state.router.roundTripper
				state.router.roundTripper = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
					if callCount == 1 {
						// Simulate a read error (not a dial error)
						return nil, &net.OpError{Op: "read", Err: errors.New("connection reset")}
					}
					return originalTransport.RoundTrip(req)
				})
			},
			check: func(t *testing.T, state *testState) {
				// Non-dial errors should not add to exclusion list
				assert.Assert(t, state.rw.Code == http.StatusBadGateway) // Error is not retried
				assert.Assert(t, len(state.excludedInstanceNamesReceived) == 1)
				assert.Assert(t, len(state.excludedInstanceNamesReceived[0]) == 0)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.MaxRoundTripAttempts = 3
			mcc := fixture.NewMockControllerClient(t)

			state := &testState{
				fn:     fixture.NewFunction(t),
				rw:     httptest.NewRecorder(),
				router: New(cfg, mcc),
				mcc:    mcc,
			}

			tc.setup(t, state)

			req := fixture.NewFunctionRequest(t, state.fn, http.MethodGet, "/", nil)
			state.router.ServeHTTP(state.rw, req)

			tc.check(t, state)
		})
	}
}

func TestContextCancellation(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		expectedErr error
	}{
		{
			name:        "context canceled",
			expectedErr: context.Canceled,
		},
		{
			name:        "context deadline exceeded",
			expectedErr: context.DeadlineExceeded,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fn := fixture.NewFunction(t)

			var ctx context.Context
			var cancel context.CancelFunc

			if tc.expectedErr == context.DeadlineExceeded {
				// Use a very short timeout that will expire during backoff
				ctx, cancel = context.WithTimeout(t.Context(), 50*time.Millisecond)
			} else {
				ctx, cancel = context.WithCancel(t.Context())
			}
			defer cancel()

			mcc := fixture.NewMockControllerClient(t)
			callCount := 0
			mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
				callCount++
				if callCount == 1 {
					// First call fails, triggering a retry with backoff
					// Cancel immediately for the canceled test case
					if tc.expectedErr == context.Canceled {
						cancel()
					}
					return nil, errors.New("temporary error")
				}
				// This should never be called if context is cancelled during backoff
				t.Error("Instance called after context should have been cancelled")
				return nil, errors.New("should not reach here")
			})

			cfg := testConfig()
			cfg.MaxRoundTripAttempts = 5
			cfg.RoundTripRetryMinTimeout = 1 * time.Second  // Long enough so context cancels first
			cfg.RoundTripRetryMaxTimeout = 10 * time.Second // Long enough so context cancels first
			router := New(cfg, mcc)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			fn.SetHeader(req)
			// RoundTrip expects the function to be in context (normally set by ServeHTTP)
			req = req.WithContext(withFunction(ctx, fn))

			_, err := router.RoundTrip(req)

			assert.Assert(t, errors.Is(err, tc.expectedErr), "expected %v, got %v", tc.expectedErr, err)
			assert.Assert(t, callCount == 1, "expected 1 call, got %d", callCount)
		})
	}
}

func TestCalculateBackoff(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		minTimeout time.Duration
		maxTimeout time.Duration
		attempt    int
		checkMin   time.Duration
		checkMax   time.Duration
	}{
		{
			name:       "attempt 1 within bounds",
			minTimeout: 100 * time.Millisecond,
			maxTimeout: 5 * time.Second,
			attempt:    1,
			checkMin:   200 * time.Millisecond,
			checkMax:   400 * time.Millisecond,
		},
		{
			name:       "attempt 2 within bounds",
			minTimeout: 100 * time.Millisecond,
			maxTimeout: 5 * time.Second,
			attempt:    2,
			checkMin:   400 * time.Millisecond,
			checkMax:   800 * time.Millisecond,
		},
		{
			name:       "attempt 3 within bounds",
			minTimeout: 100 * time.Millisecond,
			maxTimeout: 5 * time.Second,
			attempt:    3,
			checkMin:   800 * time.Millisecond,
			checkMax:   1600 * time.Millisecond,
		},
		{
			name:       "capped at max timeout",
			minTimeout: 100 * time.Millisecond,
			maxTimeout: 500 * time.Millisecond,
			attempt:    10,
			checkMin:   500 * time.Millisecond,
			checkMax:   500 * time.Millisecond,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := testConfig()
			cfg.RoundTripRetryMinTimeout = tc.minTimeout
			cfg.RoundTripRetryMaxTimeout = tc.maxTimeout
			router := New(cfg, fixture.NewMockControllerClient(t))

			// Run multiple times due to randomness
			for range 100 {
				backoff := router.calculateBackoff(tc.attempt)
				assert.Assert(t, backoff >= tc.checkMin, "attempt %d: backoff %v < min %v", tc.attempt, backoff, tc.checkMin)
				assert.Assert(t, backoff <= tc.checkMax, "attempt %d: backoff %v > max %v", tc.attempt, backoff, tc.checkMax)
			}
		})
	}
}

func TestRequestDrainingOnShutdown(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	requestStarted := make(chan struct{})
	requestCanFinish := make(chan struct{})
	requestCompleted := make(chan struct{})

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			close(requestStarted)
			<-requestCanFinish
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte("completed"))
		}), nil
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		return nil
	})

	cfg := testConfig()
	cfg.ShutdownTimeout = 5 * time.Second
	router := New(cfg, mcc)

	ctx, cancel := context.WithCancel(t.Context())
	router.Start(ctx)

	// Create a real HTTP server to test proper request draining
	server := httptest.NewServer(router)
	defer server.Close()

	// Start a request in the background
	go func() {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/", nil)
		fn.SetHeader(req)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			assert.Assert(t, resp.StatusCode == http.StatusOK)
			assert.Assert(t, string(body) == "completed")
		}
		close(requestCompleted)
	}()

	// Wait for the request to start processing
	<-requestStarted

	// Cancel the context (simulating shutdown signal)
	cancel()

	// Allow the request to complete
	close(requestCanFinish)

	// Verify the request completed successfully
	select {
	case <-requestCompleted:
		// Success - request completed despite shutdown
	case <-time.After(2 * time.Second):
		t.Fatal("request did not complete within timeout")
	}
}

func TestConcurrentRequestsSameFunction(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	const numRequests = 50
	var maxInFlight int64
	var currentInFlight int64
	var mu sync.Mutex

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			mu.Lock()
			currentInFlight++
			if currentInFlight > maxInFlight {
				maxInFlight = currentInFlight
			}
			mu.Unlock()

			// Simulate some processing time
			time.Sleep(10 * time.Millisecond)

			mu.Lock()
			currentInFlight--
			mu.Unlock()

			rw.WriteHeader(http.StatusOK)
		}), nil
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		return nil
	})

	cfg := testConfig()
	router := New(cfg, mcc)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	router.Start(ctx)

	server := httptest.NewServer(router)
	defer server.Close()

	var wg sync.WaitGroup
	errors := make(chan error, numRequests)

	for range numRequests {
		wg.Go(func() {
			req, _ := http.NewRequest(http.MethodGet, server.URL+"/", nil)
			fn.SetHeader(req)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errors <- err
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errors <- fmt.Errorf("unexpected status code: %d", resp.StatusCode)
			}
		})
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("request failed: %v", err)
	}

	// Verify concurrent execution happened
	assert.Assert(t, maxInFlight > 1, "expected concurrent execution, but max in-flight was %d", maxInFlight)

	// Verify in-flight count is back to 0
	state, ok := router.heartbeats.Load(fn.Hash())
	if ok {
		assert.Assert(t, state.inFlight.Load() == 0, "expected 0 in-flight requests, got %d", state.inFlight.Load())
	}
}

func TestHeartbeatStaleCleanup(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(http.StatusOK)
		}), nil
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		return nil
	})

	cfg := testConfig()
	// Use longer interval to reduce impact of the ±200ms jitter in timer.Loop
	cfg.HeartbeatInterval = 300 * time.Millisecond
	router := New(cfg, mcc)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	router.Start(ctx)

	// Use a real server so request context gets cancelled when request completes
	server := httptest.NewServer(router)
	defer server.Close()

	// Make a request to create a heartbeat entry
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/", nil)
	fn.SetHeader(req)
	resp, err := http.DefaultClient.Do(req)
	assert.NilError(t, err)
	resp.Body.Close()
	assert.Assert(t, resp.StatusCode == http.StatusOK)

	// Verify heartbeat exists
	_, ok := router.heartbeats.Load(fn.Hash())
	assert.Assert(t, ok, "expected heartbeat to exist after request")

	// Wait for more than 3x heartbeat interval for stale cleanup, plus buffer for jitter and loop
	// Cleanup happens when timestamp is older than 3 intervals (900ms), plus up to 200ms jitter
	// Then the heartbeat loop needs to run again (another interval + jitter)
	time.Sleep(cfg.HeartbeatInterval*4 + 500*time.Millisecond)

	// Verify heartbeat was cleaned up
	_, ok = router.heartbeats.Load(fn.Hash())
	assert.Assert(t, !ok, "expected heartbeat to be cleaned up after becoming stale")
}

func TestHeartbeatControllerError(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	heartbeatCalls := make(chan struct{}, 10)
	requestHolding := make(chan struct{})
	releaseRequest := make(chan struct{})

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			select {
			case requestHolding <- struct{}{}:
			default:
			}
			<-releaseRequest
			rw.WriteHeader(http.StatusOK)
		}), nil
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		select {
		case heartbeatCalls <- struct{}{}:
		default:
		}
		// Return an error to simulate controller failure
		return errors.New("controller unavailable")
	})

	cfg := testConfig()
	cfg.HeartbeatInterval = 50 * time.Millisecond
	router := New(cfg, mcc)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	router.Start(ctx)

	// Start a request in the background
	server := httptest.NewServer(router)
	defer server.Close()

	go func() {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/", nil)
		fn.SetHeader(req)
		http.DefaultClient.Do(req) //nolint:errcheck
	}()

	// Wait for request to be held
	<-requestHolding

	// Wait for multiple heartbeat attempts despite errors
	heartbeatCount := 0
	timeout := time.After(500 * time.Millisecond)
	for heartbeatCount < 3 {
		select {
		case <-heartbeatCalls:
			heartbeatCount++
		case <-timeout:
			t.Fatalf("expected at least 3 heartbeat calls, got %d", heartbeatCount)
		}
	}

	// Release the request
	close(releaseRequest)
}

func TestStartContextCancellationStopsHeartbeats(t *testing.T) {
	t.Parallel()

	heartbeatCalls := make(chan struct{}, 100)

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		select {
		case heartbeatCalls <- struct{}{}:
		default:
		}
		return nil
	})

	cfg := testConfig()
	cfg.HeartbeatInterval = 20 * time.Millisecond
	router := New(cfg, mcc)

	ctx, cancel := context.WithCancel(t.Context())
	router.Start(ctx)

	// Wait for some heartbeats
	time.Sleep(100 * time.Millisecond)

	// Count heartbeats before cancel
	countBefore := len(heartbeatCalls)
	assert.Assert(t, countBefore > 0, "expected some heartbeats before cancel")

	// Cancel the context
	cancel()

	// Wait a bit for cancellation to propagate
	time.Sleep(50 * time.Millisecond)

	// Drain and count heartbeats after cancel
	countAfter := len(heartbeatCalls)

	// Wait more to ensure no new heartbeats
	time.Sleep(100 * time.Millisecond)
	finalCount := len(heartbeatCalls)

	assert.Assert(t, finalCount == countAfter, "expected no new heartbeats after cancel, got %d more", finalCount-countAfter)
}

func TestResponseStatusCodes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		statusCode int
		body       string
	}{
		{"200 OK", http.StatusOK, "success"},
		{"201 Created", http.StatusCreated, "created"},
		{"204 No Content", http.StatusNoContent, ""},
		{"400 Bad Request", http.StatusBadRequest, "bad request"},
		{"401 Unauthorized", http.StatusUnauthorized, "unauthorized"},
		{"403 Forbidden", http.StatusForbidden, "forbidden"},
		{"404 Not Found", http.StatusNotFound, "not found"},
		{"500 Internal Server Error", http.StatusInternalServerError, "internal error"},
		{"502 Bad Gateway", http.StatusBadGateway, "bad gateway"},
		{"503 Service Unavailable", http.StatusServiceUnavailable, "service unavailable"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fn := fixture.NewFunction(t)

			mcc := fixture.NewMockControllerClient(t)
			mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
				return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
					rw.WriteHeader(tc.statusCode)
					if tc.body != "" {
						rw.Write([]byte(tc.body))
					}
				}), nil
			})

			rw := httptest.NewRecorder()
			req := fixture.NewFunctionRequest(t, fn, http.MethodGet, "/", nil)

			router := New(testConfig(), mcc)
			router.ServeHTTP(rw, req)

			assert.Assert(t, rw.Code == tc.statusCode, "expected status %d, got %d", tc.statusCode, rw.Code)
			assert.Assert(t, rw.Body.String() == tc.body, "expected body %q, got %q", tc.body, rw.Body.String())
		})
	}
}

func TestResponseHeaders(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			rw.Header().Set("X-Custom-Response", "custom-value")
			rw.Header().Set("Content-Type", "application/json")
			rw.Header().Add("X-Multi-Value", "value1")
			rw.Header().Add("X-Multi-Value", "value2")
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte(`{"status":"ok"}`))
		}), nil
	})

	rw := httptest.NewRecorder()
	req := fixture.NewFunctionRequest(t, fn, http.MethodGet, "/", nil)

	router := New(testConfig(), mcc)
	router.ServeHTTP(rw, req)

	assert.Assert(t, rw.Code == http.StatusOK)
	assert.Assert(t, rw.Header().Get("X-Custom-Response") == "custom-value")
	assert.Assert(t, rw.Header().Get("Content-Type") == "application/json")
	assert.DeepEqual(t, rw.Header().Values("X-Multi-Value"), []string{"value1", "value2"})
}

func TestQueryStringPreservation(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	var receivedQuery string

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			receivedQuery = req.URL.RawQuery
			rw.WriteHeader(http.StatusOK)
		}), nil
	})

	rw := httptest.NewRecorder()
	req := fixture.NewFunctionRequest(t, fn, http.MethodGet, "/?foo=bar&baz=qux&special=%20encoded", nil)

	router := New(testConfig(), mcc)
	router.ServeHTTP(rw, req)

	assert.Assert(t, rw.Code == http.StatusOK)
	assert.Assert(t, receivedQuery == "foo=bar&baz=qux&special=%20encoded", "expected query string to be preserved, got %q", receivedQuery)
}

func TestHeartbeatBatching(t *testing.T) {
	t.Parallel()

	fn1 := fixture.NewFunction(t)
	fn2 := fixture.NewFunction(t)
	fn3 := fixture.NewFunction(t)

	requestsHolding := make(chan struct{}, 3)
	releaseRequests := make(chan struct{})

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			requestsHolding <- struct{}{}
			<-releaseRequests
			rw.WriteHeader(http.StatusOK)
		}), nil
	})

	var maxBatchSize int
	var mu sync.Mutex
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		mu.Lock()
		if len(heartbeats) > maxBatchSize {
			maxBatchSize = len(heartbeats)
		}
		mu.Unlock()
		return nil
	})

	cfg := testConfig()
	// Use longer interval to account for ±200ms jitter in timer.Loop
	cfg.HeartbeatInterval = 300 * time.Millisecond
	router := New(cfg, mcc)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	router.Start(ctx)

	server := httptest.NewServer(router)
	defer server.Close()

	// Start 3 requests for different functions concurrently
	var wg sync.WaitGroup
	for _, fn := range []*skipper.Function{fn1, fn2, fn3} {
		wg.Go(func() {
			req, _ := http.NewRequest(http.MethodGet, server.URL+"/", nil)
			fn.SetHeader(req)
			http.DefaultClient.Do(req) //nolint:errcheck
		})
	}

	// Wait for all 3 requests to be holding
	for range 3 {
		<-requestsHolding
	}

	// Wait for heartbeats to be registered and the loop to run multiple times
	// Account for ±200ms jitter in timer.Loop
	time.Sleep(cfg.HeartbeatInterval*3 + 500*time.Millisecond)

	mu.Lock()
	batchSize := maxBatchSize
	mu.Unlock()

	// Release all requests and wait for them to complete
	close(releaseRequests)
	wg.Wait()

	assert.Assert(t, batchSize >= 3, "expected heartbeats to be batched with at least 3 entries, got %d", batchSize)
}

func TestLargeRequestBody(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	// 1MB body
	largeBody := strings.Repeat("x", 1024*1024)
	var receivedBody string

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				rw.WriteHeader(http.StatusInternalServerError)
				return
			}
			receivedBody = string(body)
			rw.WriteHeader(http.StatusOK)
		}), nil
	})

	rw := httptest.NewRecorder()
	req := fixture.NewFunctionRequest(t, fn, http.MethodPost, "/", strings.NewReader(largeBody))
	req.Header.Set("Content-Type", "application/octet-stream")

	router := New(testConfig(), mcc)
	router.ServeHTTP(rw, req)

	assert.Assert(t, rw.Code == http.StatusOK)
	assert.Assert(t, len(receivedBody) == len(largeBody), "expected body length %d, got %d", len(largeBody), len(receivedBody))
}

func TestLargeResponseBody(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	// 1MB response
	largeResponse := strings.Repeat("y", 1024*1024)

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			rw.Header().Set("Content-Type", "application/octet-stream")
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte(largeResponse))
		}), nil
	})

	rw := httptest.NewRecorder()
	req := fixture.NewFunctionRequest(t, fn, http.MethodGet, "/", nil)

	router := New(testConfig(), mcc)
	router.ServeHTTP(rw, req)

	assert.Assert(t, rw.Code == http.StatusOK)
	assert.Assert(t, rw.Body.Len() == len(largeResponse), "expected body length %d, got %d", len(largeResponse), rw.Body.Len())
}

func TestMultipleConcurrentRequestsDrainOnShutdown(t *testing.T) {
	t.Parallel()

	const numRequests = 10
	requestsStarted := make(chan struct{}, numRequests)
	requestsCanFinish := make(chan struct{})
	requestResults := make(chan error, numRequests)

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			requestsStarted <- struct{}{}
			<-requestsCanFinish
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte("completed"))
		}), nil
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		return nil
	})

	cfg := testConfig()
	cfg.ShutdownTimeout = 10 * time.Second
	router := New(cfg, mcc)

	ctx, cancel := context.WithCancel(t.Context())
	router.Start(ctx)

	server := httptest.NewServer(router)
	defer server.Close()

	// Start multiple concurrent requests
	var wg sync.WaitGroup
	for range numRequests {
		fn := fixture.NewFunction(t)
		wg.Go(func() {
			req, _ := http.NewRequest(http.MethodGet, server.URL+"/", nil)
			fn.SetHeader(req)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				requestResults <- err
				return
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK || string(body) != "completed" {
				requestResults <- errors.New("unexpected response")
				return
			}
			requestResults <- nil
		})
	}

	// Wait for all requests to start processing
	for range numRequests {
		<-requestsStarted
	}

	// Cancel the context (simulating shutdown signal) while requests are in-flight
	cancel()

	// Allow all requests to complete
	close(requestsCanFinish)

	// Wait for all requests to finish
	wg.Wait()
	close(requestResults)

	// Verify all requests completed successfully
	var failedCount int
	for err := range requestResults {
		if err != nil {
			failedCount++
			t.Logf("request failed: %v", err)
		}
	}
	assert.Assert(t, failedCount == 0, "expected all %d requests to complete, but %d failed", numRequests, failedCount)
}

func TestShutdownTimeoutExceeded(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	requestStarted := make(chan struct{})
	requestCanFinish := make(chan struct{})

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			close(requestStarted)
			<-requestCanFinish
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte("completed"))
		}), nil
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		return nil
	})

	cfg := testConfig()
	cfg.ShutdownTimeout = 50 * time.Millisecond // Very short timeout
	router := New(cfg, mcc)

	ctx, cancel := context.WithCancel(t.Context())
	router.Start(ctx)

	server := httptest.NewServer(router)
	defer server.Close()

	// Start a request that will take longer than shutdown timeout
	requestCompleted := make(chan struct {
		statusCode int
		body       string
		err        error
	}, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/", nil)
		fn.SetHeader(req)
		resp, err := http.DefaultClient.Do(req)
		result := struct {
			statusCode int
			body       string
			err        error
		}{err: err}
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			result.statusCode = resp.StatusCode
			result.body = string(body)
		}
		requestCompleted <- result
	}()

	// Wait for request to start
	<-requestStarted

	// Trigger shutdown - this will timeout because request takes longer
	cancel()

	// Wait longer than shutdown timeout
	time.Sleep(cfg.ShutdownTimeout + 50*time.Millisecond)

	// Now let the request complete
	close(requestCanFinish)

	// Verify the request eventually completes
	// (Go's http.Server.Shutdown doesn't force-close connections on timeout)
	select {
	case result := <-requestCompleted:
		// Request may succeed or fail depending on timing
		// The key is that shutdown timeout was exceeded
		if result.err == nil {
			assert.Assert(t, result.statusCode == http.StatusOK, "expected 200, got %d", result.statusCode)
		}
		// If there was an error, that's also acceptable - the connection may have been closed
	case <-time.After(5 * time.Second):
		t.Fatal("request did not complete within expected time")
	}
}

func TestNewRequestsRejectedDuringShutdown(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)

	mcc := fixture.NewMockControllerClient(t)
	// Instance won't be called since server is closed before request
	// Heartbeat might be called during startup, so we mock it
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		return nil
	})

	cfg := testConfig()
	cfg.ShutdownTimeout = 5 * time.Second
	cfg.HeartbeatInterval = 20 * time.Millisecond // Short so it gets called before we close
	router := New(cfg, mcc)

	ctx, cancel := context.WithCancel(t.Context())
	router.Start(ctx)

	server := httptest.NewServer(router)

	// Wait for at least one heartbeat call to satisfy the mock
	time.Sleep(cfg.HeartbeatInterval + 50*time.Millisecond)

	// Create a custom HTTP client with shorter timeouts
	client := &http.Client{
		Timeout: 1 * time.Second,
	}

	// Close the server
	cancel()
	server.Close()

	// Try to make a new request after server is closed
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/", nil)
	fn.SetHeader(req)
	_, err := client.Do(req)

	// The request should fail since the server is closed
	assert.Assert(t, err != nil, "expected error when making request after shutdown")
}

func TestHeartbeatTimestampUpdatedDuringLongRequest(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	requestStarted := make(chan struct{})
	requestCanFinish := make(chan struct{})

	// Track heartbeat timestamps for heartbeats that have in-flight requests
	var heartbeatTimestamps []time.Time
	var heartbeatMu sync.Mutex

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			close(requestStarted)
			<-requestCanFinish
			rw.WriteHeader(http.StatusOK)
		}), nil
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		heartbeatMu.Lock()
		defer heartbeatMu.Unlock()
		// Track any heartbeat with in-flight requests (our test request)
		for _, hb := range heartbeats {
			if hb.GetInFlightRequests() > 0 {
				heartbeatTimestamps = append(heartbeatTimestamps, hb.GetTimestamp().AsTime())
			}
		}
		return nil
	})

	cfg := testConfig()
	// Use a longer interval to make the test more robust
	cfg.HeartbeatInterval = 100 * time.Millisecond
	router := New(cfg, mcc)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	router.Start(ctx)

	server := httptest.NewServer(router)
	defer server.Close()

	// Start a long-running request
	go func() {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/", nil)
		fn.SetHeader(req)
		http.DefaultClient.Do(req) //nolint:errcheck
	}()

	// Wait for request to start
	<-requestStarted

	// Wait for multiple heartbeat intervals (with buffer for jitter)
	time.Sleep(cfg.HeartbeatInterval*6 + 300*time.Millisecond)

	// Allow request to complete
	close(requestCanFinish)

	// Give time for final heartbeats
	time.Sleep(100 * time.Millisecond)

	heartbeatMu.Lock()
	timestamps := make([]time.Time, len(heartbeatTimestamps))
	copy(timestamps, heartbeatTimestamps)
	heartbeatMu.Unlock()

	// Verify we got multiple heartbeats with increasing timestamps
	assert.Assert(t, len(timestamps) >= 3, "expected at least 3 heartbeats, got %d", len(timestamps))

	// Verify timestamps are increasing (heartbeat was updated during request)
	for i := 1; i < len(timestamps); i++ {
		assert.Assert(t, !timestamps[i].Before(timestamps[i-1]),
			"heartbeat timestamps should be non-decreasing: %v -> %v", timestamps[i-1], timestamps[i])
	}
}

func TestHeartbeatInFlightAccuracyDuringConcurrentRequests(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	const numRequests = 5
	requestsStarted := make(chan struct{}, numRequests)
	requestCanFinish := make(chan struct{})

	// Track in-flight counts from heartbeats
	var inFlightCounts []uint32
	var inFlightMu sync.Mutex

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			requestsStarted <- struct{}{}
			<-requestCanFinish
			rw.WriteHeader(http.StatusOK)
		}), nil
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		inFlightMu.Lock()
		defer inFlightMu.Unlock()
		for _, hb := range heartbeats {
			if hb.GetFunction().Hash() == fn.Hash() {
				inFlightCounts = append(inFlightCounts, hb.GetInFlightRequests())
			}
		}
		return nil
	})

	cfg := testConfig()
	cfg.HeartbeatInterval = 50 * time.Millisecond
	router := New(cfg, mcc)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	router.Start(ctx)

	server := httptest.NewServer(router)
	defer server.Close()

	// Start concurrent requests
	var wg sync.WaitGroup
	for range numRequests {
		wg.Go(func() {
			req, _ := http.NewRequest(http.MethodGet, server.URL+"/", nil)
			fn.SetHeader(req)
			http.DefaultClient.Do(req) //nolint:errcheck
		})
	}

	// Wait for all requests to start
	for range numRequests {
		<-requestsStarted
	}

	// Wait for a heartbeat that sees all requests in-flight. Heartbeats may fire during
	// ramp-up and see fewer requests, so poll until we observe the expected count.
	poll.WaitOn(t, func(t poll.LogT) poll.Result {
		inFlightMu.Lock()
		defer inFlightMu.Unlock()
		if slices.Contains(inFlightCounts, uint32(numRequests)) {
			return poll.Success()
		}
		return poll.Continue("waiting for heartbeat with %d in-flight, recordings so far: %v", numRequests, inFlightCounts)
	}, poll.WithDelay(10*time.Millisecond), poll.WithTimeout(5*time.Second))

	// Release all requests
	close(requestCanFinish)
	wg.Wait()

	inFlightMu.Lock()
	counts := make([]uint32, len(inFlightCounts))
	copy(counts, inFlightCounts)
	inFlightMu.Unlock()

	// Verify the count never exceeds expected (thread safety)
	for _, count := range counts {
		assert.Assert(t, count <= uint32(numRequests),
			"in-flight count %d exceeded max expected %d", count, numRequests)
	}
}

func TestStreamingResponseDuringShutdown(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	streamStarted := make(chan struct{})
	streamCanContinue := make(chan struct{})
	expectedChunks := 5

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			// Enable chunked transfer encoding
			rw.Header().Set("Transfer-Encoding", "chunked")
			rw.WriteHeader(http.StatusOK)

			// Signal that streaming has started
			close(streamStarted)

			// Wait for signal to continue
			<-streamCanContinue

			// Write chunks with flushing
			flusher, ok := rw.(http.Flusher)
			for i := range expectedChunks {
				fmt.Fprintf(rw, "chunk-%d\n", i)
				if ok {
					flusher.Flush()
				}
				time.Sleep(10 * time.Millisecond)
			}
		}), nil
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		return nil
	})

	cfg := testConfig()
	cfg.ShutdownTimeout = 5 * time.Second
	router := New(cfg, mcc)

	ctx, cancel := context.WithCancel(t.Context())
	router.Start(ctx)

	server := httptest.NewServer(router)
	defer server.Close()

	// Start streaming request
	responseComplete := make(chan struct {
		body string
		err  error
	}, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/", nil)
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
		responseComplete <- result
	}()

	// Wait for streaming to start
	<-streamStarted

	// Initiate shutdown while streaming
	cancel()

	// Allow streaming to continue
	close(streamCanContinue)

	// Verify full response is received
	select {
	case result := <-responseComplete:
		assert.NilError(t, result.err)
		// Verify all chunks were received
		for i := range expectedChunks {
			expected := fmt.Sprintf("chunk-%d", i)
			assert.Assert(t, strings.Contains(result.body, expected),
				"expected body to contain %q, got %q", expected, result.body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("streaming response did not complete within timeout")
	}
}

func TestRequestBodyReusedAcrossRetries(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	expectedBody := "this body should be readable on retry"

	// Track how many times the body was successfully read
	var bodyReadCount int
	var bodyReadMu sync.Mutex

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			body, err := io.ReadAll(req.Body)
			if err == nil && string(body) == expectedBody {
				bodyReadMu.Lock()
				bodyReadCount++
				bodyReadMu.Unlock()
			}
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte("success"))
		}), nil
	})

	cfg := testConfig()
	cfg.MaxRoundTripAttempts = 3
	cfg.RoundTripRetryMinTimeout = 10 * time.Millisecond
	cfg.RoundTripRetryMaxTimeout = 50 * time.Millisecond
	router := New(cfg, mcc)

	// Inject a dial error on the first attempt
	roundTripCount := 0
	originalTransport := router.roundTripper
	router.roundTripper = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		roundTripCount++
		if roundTripCount == 1 {
			// Simulate dial error on first attempt - body should be reusable
			return nil, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
		}
		return originalTransport.RoundTrip(req)
	})

	body := &noReadAfterClose{ReadCloser: io.NopCloser(strings.NewReader(expectedBody))}
	req := fixture.NewFunctionRequest(t, fn, http.MethodPost, "/", body)
	rw := httptest.NewRecorder()

	router.ServeHTTP(rw, req)

	assert.Assert(t, rw.Code == http.StatusOK, "expected 200, got %d", rw.Code)
	assert.Assert(t, rw.Body.String() == "success", "expected 'success', got %q", rw.Body.String())

	bodyReadMu.Lock()
	count := bodyReadCount
	bodyReadMu.Unlock()
	assert.Assert(t, count == 1, "expected body to be read once on successful retry, got %d", count)
	assert.Assert(t, body.closed, "expected body to be closed after request")
}

func TestControllerUnavailableMidRequest(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	requestStarted := make(chan struct{})
	requestCanFinish := make(chan struct{})

	// Track heartbeat calls and errors
	var heartbeatCallCount int
	var heartbeatErrorCount int
	var heartbeatMu sync.Mutex
	failHeartbeats := false

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			close(requestStarted)
			<-requestCanFinish
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte("completed"))
		}), nil
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		heartbeatMu.Lock()
		defer heartbeatMu.Unlock()
		heartbeatCallCount++
		if failHeartbeats {
			heartbeatErrorCount++
			return errors.New("controller unavailable")
		}
		return nil
	})

	cfg := testConfig()
	// Use a short interval but account for jitter
	cfg.HeartbeatInterval = 50 * time.Millisecond
	router := New(cfg, mcc)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	router.Start(ctx)

	server := httptest.NewServer(router)
	defer server.Close()

	// Start a request
	requestCompleted := make(chan struct {
		statusCode int
		body       string
		err        error
	}, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/", nil)
		fn.SetHeader(req)
		resp, err := http.DefaultClient.Do(req)
		result := struct {
			statusCode int
			body       string
			err        error
		}{err: err}
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			result.statusCode = resp.StatusCode
			result.body = string(body)
		}
		requestCompleted <- result
	}()

	// Wait for request to start
	<-requestStarted

	// Wait for at least one successful heartbeat first
	time.Sleep(cfg.HeartbeatInterval*2 + 100*time.Millisecond)

	// Make controller heartbeats start failing
	heartbeatMu.Lock()
	failHeartbeats = true
	heartbeatMu.Unlock()

	// Wait for some heartbeats to fail while request is in-flight
	time.Sleep(cfg.HeartbeatInterval*4 + 200*time.Millisecond)

	// Allow request to complete
	close(requestCanFinish)

	// Verify the request completed successfully despite heartbeat failures
	select {
	case result := <-requestCompleted:
		assert.NilError(t, result.err)
		assert.Assert(t, result.statusCode == http.StatusOK, "expected 200, got %d", result.statusCode)
		assert.Assert(t, result.body == "completed", "expected 'completed', got %q", result.body)
	case <-time.After(5 * time.Second):
		t.Fatal("request did not complete within timeout")
	}

	// Verify some heartbeat errors occurred
	heartbeatMu.Lock()
	errorCount := heartbeatErrorCount
	totalCalls := heartbeatCallCount
	heartbeatMu.Unlock()
	assert.Assert(t, totalCalls > 0, "expected some heartbeat calls, got 0")
	assert.Assert(t, errorCount > 0, "expected some heartbeat errors, got 0 (total calls: %d)", totalCalls)
}

func TestRequestDrainingDuringInstanceFetch(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	instanceFetchStarted := make(chan struct{})
	instanceFetchCanFinish := make(chan struct{})
	requestCompleted := make(chan struct {
		statusCode int
		body       string
		err        error
	}, 1)

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		// Signal that we're waiting for an instance
		select {
		case instanceFetchStarted <- struct{}{}:
		default:
		}
		// Block until test allows us to continue
		select {
		case <-instanceFetchCanFinish:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte("completed"))
		}), nil
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		return nil
	})

	cfg := testConfig()
	cfg.ShutdownTimeout = 5 * time.Second
	router := New(cfg, mcc)

	ctx, cancel := context.WithCancel(t.Context())
	router.Start(ctx)

	server := httptest.NewServer(router)
	defer server.Close()

	// Start a request that will block during instance fetch
	go func() {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/", nil)
		fn.SetHeader(req)
		resp, err := http.DefaultClient.Do(req)
		result := struct {
			statusCode int
			body       string
			err        error
		}{err: err}
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			result.statusCode = resp.StatusCode
			result.body = string(body)
		}
		requestCompleted <- result
	}()

	// Wait for the instance fetch to start
	<-instanceFetchStarted

	// Cancel the context (simulating shutdown) while blocked on instance fetch
	cancel()

	// Allow the instance fetch to complete
	close(instanceFetchCanFinish)

	// Verify the request completes (either successfully or with context error)
	select {
	case result := <-requestCompleted:
		// Either outcome is acceptable - request may complete successfully
		// if it gets an instance before context is fully cancelled,
		// or it may fail with a connection error
		if result.err == nil {
			assert.Assert(t, result.statusCode == http.StatusOK, "expected 200, got %d", result.statusCode)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request did not complete within timeout")
	}

	// Verify in-flight count returns to 0
	state, ok := router.heartbeats.Load(fn.Hash())
	if ok {
		assert.Assert(t, state.inFlight.Load() == 0, "expected 0 in-flight requests, got %d", state.inFlight.Load())
	}
}

func TestBackoffDoesNotOverflowAtHighAttempts(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.RoundTripRetryMinTimeout = 100 * time.Millisecond
	cfg.RoundTripRetryMaxTimeout = 5 * time.Second
	router := New(cfg, fixture.NewMockControllerClient(t))

	// Test various high attempt numbers that could cause overflow
	testAttempts := []int{10, 50, 100, 1000, 10000}

	for _, attempt := range testAttempts {
		// Run multiple times due to randomness in backoff calculation
		for range 10 {
			backoff := router.calculateBackoff(attempt)

			// Verify backoff is within valid range
			assert.Assert(t, backoff >= 0, "attempt %d: backoff should not be negative: %v", attempt, backoff)
			assert.Assert(t, backoff <= cfg.RoundTripRetryMaxTimeout, "attempt %d: backoff %v exceeds max %v", attempt, backoff, cfg.RoundTripRetryMaxTimeout)
		}
	}
}

func TestZeroInFlightAfterAllRequestsComplete(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	const numRequests = 100

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			// Small delay to ensure concurrent execution
			time.Sleep(5 * time.Millisecond)
			rw.WriteHeader(http.StatusOK)
		}), nil
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		return nil
	})

	cfg := testConfig()
	router := New(cfg, mcc)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	router.Start(ctx)

	server := httptest.NewServer(router)
	defer server.Close()

	var wg sync.WaitGroup
	errors := make(chan error, numRequests)

	for range numRequests {
		wg.Go(func() {
			req, _ := http.NewRequest(http.MethodGet, server.URL+"/", nil)
			fn.SetHeader(req)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errors <- err
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errors <- fmt.Errorf("unexpected status: %d", resp.StatusCode)
			}
		})
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("request failed: %v", err)
	}

	// Verify in-flight count is exactly 0
	state, ok := router.heartbeats.Load(fn.Hash())
	if ok {
		assert.Assert(t, state.inFlight.Load() == 0,
			"expected exactly 0 in-flight requests after all requests complete, got %d",
			state.inFlight.Load())
	}
}

func TestInstanceCallContextCancellation(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	instanceCallStarted := make(chan struct{})

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		close(instanceCallStarted)
		// Block until context is cancelled
		<-ctx.Done()
		return nil, ctx.Err()
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		return nil
	})

	cfg := testConfig()
	router := New(cfg, mcc)

	ctx, cancel := context.WithCancel(t.Context())
	router.Start(ctx)

	server := httptest.NewServer(router)
	defer server.Close()

	// Create a request with a cancellable context
	reqCtx, reqCancel := context.WithCancel(t.Context())
	req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, server.URL+"/", nil)
	fn.SetHeader(req)

	requestCompleted := make(chan error, 1)
	go func() {
		_, err := http.DefaultClient.Do(req)
		requestCompleted <- err
	}()

	// Wait for the instance call to start
	<-instanceCallStarted

	// Cancel the request context
	reqCancel()

	// Verify the request fails with context cancellation
	select {
	case err := <-requestCompleted:
		assert.Assert(t, err != nil, "expected error when context is cancelled")
	case <-time.After(5 * time.Second):
		t.Fatal("request did not complete within timeout")
	}

	cancel() // cleanup router context
}

func TestHeartbeatNotSentAfterContextCancellation(t *testing.T) {
	t.Parallel()

	var heartbeatCount int64
	var heartbeatMu sync.Mutex

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		heartbeatMu.Lock()
		heartbeatCount++
		heartbeatMu.Unlock()
		return nil
	})

	cfg := testConfig()
	cfg.HeartbeatInterval = 50 * time.Millisecond
	router := New(cfg, mcc)

	ctx, cancel := context.WithCancel(t.Context())
	router.Start(ctx)

	// Wait for a few heartbeats
	time.Sleep(cfg.HeartbeatInterval*3 + 50*time.Millisecond)

	heartbeatMu.Lock()
	countBeforeCancel := heartbeatCount
	heartbeatMu.Unlock()
	assert.Assert(t, countBeforeCancel > 0, "expected some heartbeats before cancel")

	// Cancel the context
	cancel()

	// Wait a bit to ensure cancellation is processed
	time.Sleep(50 * time.Millisecond)

	// Record count right after cancel
	heartbeatMu.Lock()
	countAfterCancel := heartbeatCount
	heartbeatMu.Unlock()

	// Wait for 3 more heartbeat intervals
	time.Sleep(cfg.HeartbeatInterval * 4)

	// Verify no new heartbeats were sent
	heartbeatMu.Lock()
	finalCount := heartbeatCount
	heartbeatMu.Unlock()

	assert.Assert(t, finalCount == countAfterCancel,
		"expected no heartbeats after cancel, but count went from %d to %d",
		countAfterCancel, finalCount)
}

func TestRequestBodyReadErrorDuringRetry(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	var attemptCount int

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		attemptCount++
		if attemptCount == 1 {
			// First attempt - return an instance that will fail with dial error
			instance := &skipper.Instance{}
			instance.SetName("failing-instance")
			instance.SetAddr("192.0.2.1:9999") // Non-routable address
			return instance, nil
		}
		// Second attempt - return a working instance
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			// Read the body to verify it was preserved across retry
			body, err := io.ReadAll(req.Body)
			if err != nil {
				rw.WriteHeader(http.StatusInternalServerError)
				rw.Write([]byte("failed to read body: " + err.Error()))
				return
			}
			rw.WriteHeader(http.StatusOK)
			rw.Write(body)
		}), nil
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		return nil
	})

	cfg := testConfig()
	cfg.MaxRoundTripAttempts = 3
	router := New(cfg, mcc)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	router.Start(ctx)

	server := httptest.NewServer(router)
	defer server.Close()

	// Make a request with a body
	bodyContent := "test body content"
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/", strings.NewReader(bodyContent))
	fn.SetHeader(req)

	resp, err := http.DefaultClient.Do(req)
	assert.NilError(t, err)
	defer resp.Body.Close()

	// The request should succeed on retry and echo back the body
	respBody, _ := io.ReadAll(resp.Body)
	assert.Assert(t, resp.StatusCode == http.StatusOK, "expected 200, got %d: %s", resp.StatusCode, string(respBody))
	assert.Assert(t, string(respBody) == bodyContent, "body was not preserved across retry: got %q", string(respBody))
}

func TestAllInstancesExhaustedReturnsError(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	var attemptCount int

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		attemptCount++
		// Always return an instance at a non-routable address
		instance := &skipper.Instance{}
		instance.SetName(fmt.Sprintf("failing-instance-%d", attemptCount))
		instance.SetAddr("192.0.2.1:9999") // Non-routable address
		return instance, nil
	})
	mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
		return nil
	})

	cfg := testConfig()
	cfg.MaxRoundTripAttempts = 3
	cfg.RoundTripRetryMinTimeout = 1 * time.Millisecond // Speed up test
	cfg.RoundTripRetryMaxTimeout = 10 * time.Millisecond
	router := New(cfg, mcc)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	router.Start(ctx)

	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/", nil)
	fn.SetHeader(req)

	resp, err := http.DefaultClient.Do(req)
	assert.NilError(t, err)
	defer resp.Body.Close()

	// Should get a 502 Bad Gateway after exhausting retries
	assert.Assert(t, resp.StatusCode == http.StatusBadGateway, "expected 502, got %d", resp.StatusCode)

	// Verify we made the expected number of attempts
	assert.Assert(t, attemptCount == cfg.MaxRoundTripAttempts,
		"expected %d attempts, got %d", cfg.MaxRoundTripAttempts, attemptCount)
}

func TestRapidRequestsDuringShutdownWindow(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	var requestsStarted int64
	var requestsCompleted int64

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			atomic.AddInt64(&requestsStarted, 1)
			time.Sleep(50 * time.Millisecond) // Simulate processing
			atomic.AddInt64(&requestsCompleted, 1)
			rw.WriteHeader(http.StatusOK)
		}), nil
	})
	// Whether the heartbeat loop fires before the test cancels the
	// context is timing-dependent (HeartbeatInterval=100ms vs cancel
	// at 10ms), so the call may or may not happen.
	mcc.AllowHeartbeat()

	cfg := testConfig()
	cfg.ShutdownTimeout = 5 * time.Second
	router := New(cfg, mcc)

	ctx, cancel := context.WithCancel(t.Context())
	router.Start(ctx)

	server := httptest.NewServer(router)
	defer server.Close()

	const numRequests = 20
	var wg sync.WaitGroup
	results := make(chan error, numRequests)

	// Start requests rapidly
	for range numRequests {
		wg.Go(func() {
			req, _ := http.NewRequest(http.MethodGet, server.URL+"/", nil)
			fn.SetHeader(req)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				results <- err
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				results <- fmt.Errorf("unexpected status: %d", resp.StatusCode)
				return
			}
			results <- nil
		})
	}

	// Wait for some requests to start, then trigger shutdown
	time.Sleep(10 * time.Millisecond)
	cancel()

	// Wait for all requests to complete
	wg.Wait()
	close(results)

	var failures int
	for err := range results {
		if err != nil {
			failures++
		}
	}

	started := atomic.LoadInt64(&requestsStarted)
	completed := atomic.LoadInt64(&requestsCompleted)

	// Requests that started should have completed
	assert.Assert(t, started == completed,
		"all started requests should complete: started=%d completed=%d", started, completed)

	// Some requests may have failed if they didn't connect before shutdown
	// but all that started should have completed
	t.Logf("requests: started=%d completed=%d failures=%d", started, completed, failures)
}

func TestOneshotReleasesInstance(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	fn.SetOneshot(true)

	instance := fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return instance, nil
	})

	released := make(chan struct{})
	mcc.HandleReleaseInstance(func(ctx context.Context, inst *skipper.Instance) error {
		assert.Equal(t, inst.GetName(), instance.GetName())
		assert.Equal(t, inst.GetFunction().GetNamespace(), fn.GetNamespace())
		close(released)
		return nil
	})

	rw := httptest.NewRecorder()
	req := fixture.NewFunctionRequest(t, fn, http.MethodGet, "/", nil)

	router := New(testConfig(), mcc)
	router.ServeHTTP(rw, req)

	assert.Assert(t, rw.Code == http.StatusOK)

	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ReleaseInstance to be called")
	}
}

func TestOneshotReleasesInstanceOnDialError(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	fn.SetOneshot(true)

	successInstance := fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	failingInstance1 := &skipper.Instance{}
	failingInstance1.SetFunction(fn)
	failingInstance1.SetName("failing-1")
	failingInstance1.SetAddr("127.0.0.1:1")

	failingInstance2 := &skipper.Instance{}
	failingInstance2.SetFunction(fn)
	failingInstance2.SetName("failing-2")
	failingInstance2.SetAddr("127.0.0.1:2")

	callCount := 0
	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		callCount++
		switch callCount {
		case 1:
			return failingInstance1, nil
		case 2:
			return failingInstance2, nil
		default:
			return successInstance, nil
		}
	})

	var mu sync.Mutex
	releasedNames := make(map[string]bool)
	allReleased := make(chan struct{})
	mcc.HandleReleaseInstance(func(ctx context.Context, inst *skipper.Instance) error {
		mu.Lock()
		defer mu.Unlock()
		releasedNames[inst.GetName()] = true
		if len(releasedNames) == 3 {
			close(allReleased)
		}
		return nil
	})

	cfg := testConfig()
	cfg.MaxRoundTripAttempts = 4
	router := New(cfg, mcc)

	originalTransport := router.roundTripper
	roundTripCount := 0
	router.roundTripper = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		roundTripCount++
		if roundTripCount <= 2 {
			return nil, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
		}
		return originalTransport.RoundTrip(req)
	})

	rw := httptest.NewRecorder()
	req := fixture.NewFunctionRequest(t, fn, http.MethodGet, "/", nil)
	router.ServeHTTP(rw, req)

	assert.Assert(t, rw.Code == http.StatusOK)

	select {
	case <-allReleased:
	case <-time.After(5 * time.Second):
		mu.Lock()
		t.Fatalf("timed out waiting for all instances to be released, got: %v", releasedNames)
		mu.Unlock()
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Assert(t, releasedNames["failing-1"], "first failed instance should be released")
	assert.Assert(t, releasedNames["failing-2"], "second failed instance should be released")
	assert.Assert(t, releasedNames[successInstance.GetName()], "successful instance should be released")
}

// TestBodyClosedAfterServeHTTP verifies that the request body is closed after
// ServeHTTP returns, even when the transport does NOT close it. Go 1.26's
// ReverseProxy wraps the body with a noopCloseReader that doesn't propagate
// Close to the underlying body, so the router's explicit defer req.Body.Close()
// is necessary to ensure cleanup.
func TestBodyClosedAfterServeHTTP(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	expectedBody := "request-body-content"

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(http.StatusOK)
		}), nil
	})

	router := New(testConfig(), mcc)

	// Replace the round tripper with one that reads the body but does NOT
	// close it, simulating Go 1.26's noopCloseReader behavior where the
	// ReverseProxy no longer propagates Close to the original body.
	router.roundTripper = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		// Drain the body but intentionally skip req.Body.Close()
		_, _ = io.ReadAll(req.Body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})

	body := &noReadAfterClose{ReadCloser: io.NopCloser(strings.NewReader(expectedBody))}
	req := fixture.NewFunctionRequest(t, fn, http.MethodPost, "/", body)
	rw := httptest.NewRecorder()

	router.ServeHTTP(rw, req)

	assert.Assert(t, rw.Code == http.StatusOK, "expected 200, got %d", rw.Code)
	assert.Assert(t, body.closed, "expected body to be closed after ServeHTTP returns; the defer req.Body.Close() in ServeHTTP should close it even when the transport does not")
}

func BenchmarkRewriteRequestHeaders(b *testing.B) {
	b.Run("no existing headers", func(b *testing.B) {
		in := httptest.NewRequest(http.MethodGet, "/", nil)
		in.Host = "tenant.example.com"
		in.RemoteAddr = "192.0.2.1:12345"
		out := &http.Request{Header: make(http.Header)}
		pr := &httputil.ProxyRequest{In: in, Out: out}

		b.ReportAllocs()
		for b.Loop() {
			out.Header = make(http.Header)
			rewriteRequestHeaders(pr)
		}
	})

	b.Run("with forwarded-for chain", func(b *testing.B) {
		in := httptest.NewRequest(http.MethodGet, "/", nil)
		in.Host = "tenant.example.com"
		in.RemoteAddr = "192.0.2.1:12345"
		in.Header["X-Forwarded-For"] = []string{"203.0.113.50", "198.51.100.25", "10.0.0.1"}
		in.Header.Set("X-Forwarded-Host", "original.example.com")
		in.Header.Set("X-Forwarded-Proto", "https")
		out := &http.Request{Header: make(http.Header)}
		pr := &httputil.ProxyRequest{In: in, Out: out}

		b.ReportAllocs()
		for b.Loop() {
			out.Header = make(http.Header)
			rewriteRequestHeaders(pr)
		}
	})

	b.Run("ipv6", func(b *testing.B) {
		in := httptest.NewRequest(http.MethodGet, "/", nil)
		in.Host = "tenant.example.com"
		in.RemoteAddr = "[2001:db8::1]:12345"
		in.Header.Set("X-Forwarded-For", "2001:db8::1")
		out := &http.Request{Header: make(http.Header)}
		pr := &httputil.ProxyRequest{In: in, Out: out}

		b.ReportAllocs()
		for b.Loop() {
			out.Header = make(http.Header)
			rewriteRequestHeaders(pr)
		}
	})
}

var sinkStrings []string

func BenchmarkExcludedInstances(b *testing.B) {
	names := []string{
		"pod-abc12345-x7k2q",
		"pod-def67890-m3n8p",
		"pod-ghi24680-r5t1w",
		"pod-jkl13579-y9v4s",
		"pod-mno97531-b6h2j",
	}

	for _, n := range []int{1, 3, 5} {
		b.Run(fmt.Sprintf("slice/n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var excluded []string
				for i := range n {
					excluded = append(excluded, names[i])
				}
				sinkStrings = excluded
			}
		})

		b.Run(fmt.Sprintf("map/n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				excluded := make(map[string]struct{})
				for i := range n {
					excluded[names[i]] = struct{}{}
				}
				sinkStrings = slices.Collect(maps.Keys(excluded))
			}
		})
	}
}
