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

func init() {
	FlagMaxRoundTripAttempts.Init()
	FlagRoundTripRetryMinTimeout.Init()
	_ = FlagHeartbeatInterval.SetValue(100 * time.Millisecond)
	_ = FlagPodIP.SetValue(fixture.RouterIP)
	_ = FlagRoundTripRetryMaxTimeout.SetValue(10 * time.Millisecond)
}

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rw := httptest.NewRecorder()

	router := New(fixture.NewMockControllerClient(t))
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

	router := New(mockControllerClient)
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

			router := New(mcc)
			router.ServeHTTP(rw, req)

			assert.Assert(t, rw.Code == http.StatusOK)
		})

		// integration tests
		t.Run(tc.method+" integration", func(t *testing.T) {
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
	testCases := []struct {
		name         string
		setHeaders   func(fn function.Function, req *http.Request)
		checkHeaders func(t *testing.T, fn function.Function, headers http.Header)
	}{
		{
			name: "smoke",
			setHeaders: func(fn function.Function, req *http.Request) {
				req.Host = fn.Tenant + ".example.com"
			},
			checkHeaders: func(t *testing.T, fn function.Function, headers http.Header) {
				expectedHost := fn.Tenant + ".example.com"
				expectedProto := "http"

				assert.DeepEqual(t, headers.Values("Host"), []string{expectedHost})
				assert.Assert(t, len(headers.Values("X-Forwarded-For")) == 1) // TODO: check the actual value
				assert.DeepEqual(t, headers.Values("X-Forwarded-Host"), []string{expectedHost})
				assert.DeepEqual(t, headers.Values("X-Forwarded-Proto"), []string{expectedProto})
				assert.DeepEqual(t, headers.Values("Forwarded"), []string{fmt.Sprintf("for=%s;host=%s;proto=%s", headers.Get("X-Forwarded-For"), expectedHost, expectedProto)})
				assert.Assert(t, len(headers) == 5)
			},
		},
		{
			name: "custom",
			setHeaders: func(fn function.Function, req *http.Request) {
				req.Header.Set("X-Custom-Header", "custom-value")
				req.Header.Add("X-Custom-Multi-Header", "multi-value-1")
				req.Header.Add("X-Custom-Multi-Header", "multi-value-2")
			},
			checkHeaders: func(t *testing.T, fn function.Function, headers http.Header) {
				assert.DeepEqual(t, headers.Values("X-Custom-Header"), []string{"custom-value"})
				assert.DeepEqual(t, headers.Values("X-Custom-Multi-Header"), []string{"multi-value-1", "multi-value-2"})
				assert.Assert(t, len(headers) == 7)
			},
		},
	}

	for _, tc := range testCases {
		// unit tests
		t.Run(tc.name, func(t *testing.T) {
			fn := fixture.NewFunction()

			mcc := fixture.NewMockControllerClient(t)
			mcc.HandleInstance(func(ctx context.Context, fn function.Function, excludeInstanceNames ...string) (*function.Instance, error) {
				return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
					req.Header.Set("Host", req.Host) // go removes the Host header, so we manually set it back
					tc.checkHeaders(t, fn, req.Header)
				}), nil
			})

			rw := httptest.NewRecorder()
			req := fixture.NewFunctionRequest(t, fn, http.MethodGet, "/", nil)
			tc.setHeaders(fn, req)

			router := New(mcc)
			router.ServeHTTP(rw, req)

			assert.Assert(t, rw.Code == http.StatusOK)
			assert.Assert(t, rw.Body.Len() == 0)
		})

		// integration tests
		t.Run(tc.name+" integration", func(t *testing.T) {
			fn := fixture.NewEchoFunction()
			req := fixture.NewFunctionRequest(t, fn, http.MethodGet, fixture.RouterIntegrationURL, nil)
			req.Header.Set("User-Agent", "") // disable the default User-Agent header
			tc.setHeaders(fn, req)

			transport := &http.Transport{DisableCompression: true} // disable the default "Accept-Encoding: gzip" header
			res, err := transport.RoundTrip(req)
			assert.NilError(t, err)
			defer res.Body.Close()
			assert.Assert(t, res.StatusCode == http.StatusOK)

			echoResponse, err := fixture.ParseEchoResponse(res)
			assert.NilError(t, err)

			headers := echoResponse.Header()
			headers.Del("Traceparent") // ignore the Traceparent header since it may or may not be present depending on the test environment
			tc.checkHeaders(t, fn, headers)
		})
	}
}

func TestBody(t *testing.T) {
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
				assert.Assert(t, len(body) == 0)
			},
		},
		{
			name: "text",
			getBody: func() (string, io.Reader) {
				return "text/plain", strings.NewReader("hello, world!")
			},
			checkBody: func(t *testing.T, body string) {
				assert.Assert(t, body == "hello, world!")
			},
		},
		{
			name: "json",
			getBody: func() (string, io.Reader) {
				return "application/json", strings.NewReader(`{"key":"value"}`)
			},
			checkBody: func(t *testing.T, body string) {
				assert.Assert(t, body == `{"key":"value"}`)
			},
		},
	}

	fn := fixture.NewFunction()

	for _, tc := range testCases {
		// unit tests
		t.Run(tc.name, func(t *testing.T) {
			mcc := fixture.NewMockControllerClient(t)
			mcc.HandleInstance(func(ctx context.Context, fn function.Function, excludeInstanceNames ...string) (*function.Instance, error) {
				return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
					content, err := io.ReadAll(req.Body)
					assert.NilError(t, err)
					tc.checkBody(t, string(content))
				}), nil
			})

			rw := httptest.NewRecorder()

			contentType, body := tc.getBody()
			req := fixture.NewFunctionRequest(t, fn, http.MethodPost, "/", body)
			req.Header.Set("Content-Type", contentType)

			router := New(mcc)
			router.ServeHTTP(rw, req)

			assert.Assert(t, rw.Code == http.StatusOK)
		})

		// integration tests
		t.Run(tc.name+" integration", func(t *testing.T) {
			fn := fixture.NewEchoFunction()

			contentType, body := tc.getBody()
			req := fixture.NewFunctionRequest(t, fn, http.MethodPost, fixture.RouterIntegrationURL, body)
			req.Header.Set("Content-Type", contentType)

			res, err := http.DefaultTransport.RoundTrip(req)
			assert.NilError(t, err)
			defer res.Body.Close()
			assert.Assert(t, res.StatusCode == http.StatusOK)

			echoResponse, err := fixture.ParseEchoResponse(res)
			assert.NilError(t, err)

			tc.checkBody(t, echoResponse.Body)
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

	router := New(mcc)
	router.Start(ctx)
	router.ServeHTTP(rw, req)

	assert.Assert(t, rw.Code == http.StatusOK)
	assert.Assert(t, rw.Body.String() == "Hello, "+fn.Tenant)

	heartbeat, ok := router.heartbeats.Load(fn)
	assert.Assert(t, ok)
	assert.Assert(t, heartbeat.Function == fn)
	assert.Assert(t, heartbeat.Timestamp.After(testStartTime))
	assert.Assert(t, heartbeat.InFlightRequests == 0) // ensure the number of in-flight requests is 0 now that the request is complete
}

func TestRetries(t *testing.T) {
	testCases := []struct {
		name          string
		maxAttempts   int
		instanceErrs  []error
		roundTripErrs []error
		check         func(*testing.T, function.Function, *httptest.ResponseRecorder)
	}{
		{
			name:        "no errors",
			maxAttempts: 1,
			check: func(t *testing.T, fn function.Function, rw *httptest.ResponseRecorder) {
				assert.Assert(t, rw.Code == http.StatusOK)
				assert.Assert(t, rw.Body.String() == "Hello, "+fn.Tenant)
			},
		},
		{
			name:         "ctrl.Instance arbitrary error",
			maxAttempts:  2,
			instanceErrs: []error{errors.New("arbitrary error")},
			check: func(t *testing.T, fn function.Function, rw *httptest.ResponseRecorder) {
				assert.Assert(t, rw.Code == http.StatusOK)
				assert.Assert(t, rw.Body.String() == "Hello, "+fn.Tenant)
			},
		},
		{
			name:          "roundTripper dial error",
			maxAttempts:   2,
			roundTripErrs: []error{&net.OpError{Op: "dial", Err: errors.New("arbitrary error")}},
			check: func(t *testing.T, fn function.Function, rw *httptest.ResponseRecorder) {
				assert.Assert(t, rw.Code == http.StatusOK)
				assert.Assert(t, rw.Body.String() == "Hello, "+fn.Tenant)
			},
		},
		{
			name:          "ctrl.Instance and roundTripper errors",
			maxAttempts:   3,
			instanceErrs:  []error{errors.New("arbitrary error")},
			roundTripErrs: []error{&net.OpError{Op: "dial", Err: errors.New("arbitrary error")}},
			check: func(t *testing.T, fn function.Function, rw *httptest.ResponseRecorder) {
				assert.Assert(t, rw.Code == http.StatusOK)
				assert.Assert(t, rw.Body.String() == "Hello, "+fn.Tenant)
			},
		},
		{
			name:          "ctrl.Instance and roundTripper errors exceed max attempts",
			maxAttempts:   2,
			instanceErrs:  []error{errors.New("arbitrary error")},
			roundTripErrs: []error{&net.OpError{Op: "dial", Err: errors.New("arbitrary error")}},
			check: func(t *testing.T, fn function.Function, rw *httptest.ResponseRecorder) {
				assert.Assert(t, rw.Code == http.StatusBadGateway)
				assert.Assert(t, rw.Body.Len() == 0)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fixture.SetFlag(t, &FlagMaxRoundTripAttempts, tc.maxAttempts)

			fn := fixture.NewFunction()

			instanceErrsIndex := 0
			mcc := fixture.NewMockControllerClient(t)
			mcc.HandleInstance(func(ctx context.Context, fn function.Function, excludeInstanceNames ...string) (*function.Instance, error) {
				if len(tc.instanceErrs) > 0 && instanceErrsIndex < len(tc.instanceErrs) {
					instanceErrsIndex++
					return nil, tc.instanceErrs[instanceErrsIndex-1]
				}

				return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
					rw.WriteHeader(http.StatusOK)
					rw.Write([]byte("Hello, " + fn.Tenant))
				}), nil
			})

			rw := httptest.NewRecorder()
			req := fixture.NewFunctionRequest(t, fn, http.MethodGet, "/", nil)

			router := New(mcc)

			originalTransport := router.roundTripper

			roundTripperErrsIndex := 0
			router.roundTripper = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				if len(tc.roundTripErrs) > 0 && roundTripperErrsIndex < len(tc.roundTripErrs) {
					roundTripperErrsIndex++
					return nil, tc.roundTripErrs[roundTripperErrsIndex-1]
				}
				return originalTransport.RoundTrip(req)
			})

			router.ServeHTTP(rw, req)

			tc.check(t, fn, rw)
		})
	}
}

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
