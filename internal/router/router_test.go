package router

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gadget-inc/fusion/internal/fixture"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	FlagHeartbeatInterval.SetValue(time.Millisecond)
	FlagMaxRoundTripAttempts.Init()
	FlagRoundTripRetryMaxTimeout.SetValue(time.Millisecond)
	FlagRoundTripRetryMinTimeout.Init()
}

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rw := httptest.NewRecorder()

	router := New(fixture.NewMockControllerClient(t))
	router.ServeHTTP(rw, req)

	require.Equal(t, http.StatusOK, rw.Code)
	require.Empty(t, rw.Body)
}

func TestSimple(t *testing.T) {
	fn := fixture.NewFunction()

	mockControllerClient := fixture.NewMockControllerClient(t)
	mockControllerClient.HandleGet(fn, func(ctx context.Context, fn function.Function) (*function.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			require.Equal(t, req.Method, http.MethodGet)
			require.Equal(t, req.URL.Path, "/")

			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte("Hello, " + fn.Tenant))
		}), nil
	})

	rw := httptest.NewRecorder()
	req := fixture.NewFunctionRequest(t, fn, http.MethodGet, "/", nil)

	router := New(mockControllerClient)
	router.ServeHTTP(rw, req)

	require.Equal(t, http.StatusOK, rw.Code)
	require.Equal(t, "Hello, "+fn.Tenant, rw.Body.String())
}

func TestMethod(t *testing.T) {
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

	fn := fixture.NewFunction()

	for _, tc := range testCases {
		t.Run(tc.method, func(t *testing.T) {
			mcc := fixture.NewMockControllerClient(t)
			mcc.HandleGet(fn, func(ctx context.Context, fn function.Function) (*function.Instance, error) {
				return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
					assert.Equal(t, req.Method, tc.method)
				}), nil
			})

			rw := httptest.NewRecorder()
			req := fixture.NewFunctionRequest(t, fn, tc.method, "/", nil)

			router := New(mcc)
			router.ServeHTTP(rw, req)
		})
	}
}

func TestHeaders(t *testing.T) {
	testCases := []struct {
		name         string
		setHeaders   func(req *http.Request)
		checkHeaders func(t *testing.T, headers http.Header)
	}{
		{
			name:       "default",
			setHeaders: func(req *http.Request) {},
			checkHeaders: func(t *testing.T, headers http.Header) {
				// accept-encoding, x-forwarded-for, x-forwarded-host, x-forwarded-proto
				assert.Len(t, headers, 4, "unexpected number of headers")
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
				assert.Equal(t, "custom-value", headers.Get("X-Custom-Header"))
				assert.Equal(t, []string{"multi-value-1", "multi-value-2"}, headers.Values("X-Custom-Multi-Header"))
				assert.Len(t, headers, 6, "unexpected number of headers")
			},
		},
	}

	fn := fixture.NewFunction()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			host := fn.Tenant + ".example.com"

			mcc := fixture.NewMockControllerClient(t)
			mcc.HandleGet(fn, func(ctx context.Context, fn function.Function) (*function.Instance, error) {
				return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
					assert.Equal(t, host, req.Host)
					tc.checkHeaders(t, req.Header)
				}), nil
			})

			rw := httptest.NewRecorder()
			req := fixture.NewFunctionRequest(t, fn, http.MethodGet, "http://"+host, nil)
			tc.setHeaders(req)

			router := New(mcc)
			router.ServeHTTP(rw, req)
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
				assert.Empty(t, body)
			},
		},
		{
			name: "text",
			getBody: func() (string, io.Reader) {
				return "text/plain", strings.NewReader("hello, world!")
			},
			checkBody: func(t *testing.T, body string) {
				assert.Equal(t, "hello, world!", body)
			},
		},
		{
			name: "json",
			getBody: func() (string, io.Reader) {
				return "application/json", strings.NewReader(`{"key":"value"}`)
			},
			checkBody: func(t *testing.T, body string) {
				assert.Equal(t, `{"key":"value"}`, body)
			},
		},
	}

	fn := fixture.NewFunction()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mcc := fixture.NewMockControllerClient(t)
			mcc.HandleGet(fn, func(ctx context.Context, fn function.Function) (*function.Instance, error) {
				return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
					content, err := io.ReadAll(req.Body)
					assert.NoError(t, err)
					tc.checkBody(t, string(content))
				}), nil
			})

			rw := httptest.NewRecorder()

			contentType, body := tc.getBody()
			req := fixture.NewFunctionRequest(t, fn, http.MethodPost, "/", body)
			req.Header.Set("Content-Type", contentType)

			router := New(mcc)
			router.ServeHTTP(rw, req)
		})
	}
}

func TestHeartbeats(t *testing.T) {
	fn := fixture.NewFunction()

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleGet(fn, func(ctx context.Context, fn function.Function) (*function.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte("Hello, " + fn.Tenant))
		}), nil
	})

	testStartTime := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rw := httptest.NewRecorder()
	req := fixture.NewFunctionRequestWithContext(t, ctx, fn, http.MethodGet, "/", nil)

	router := New(mcc)
	router.Start(ctx)
	router.ServeHTTP(rw, req)

	require.Equal(t, http.StatusOK, rw.Code)
	require.Equal(t, "Hello, "+fn.Tenant, rw.Body.String())

	require.Eventually(t, func() bool {
		return len(mcc.Heartbeats()) > 0
	}, time.Second, time.Millisecond)

	heartbeat := mcc.Heartbeats()[0]
	require.Equal(t, fn, heartbeat.Function)
	require.True(t, heartbeat.Timestamp.After(testStartTime))
}

func TestRetries(t *testing.T) {
	testCases := []struct {
		name          string
		maxAttempts   int
		getErrs       []error
		roundTripErrs []error
		check         func(*testing.T, function.Function, *httptest.ResponseRecorder)
	}{
		{
			name:        "no errors",
			maxAttempts: 1,
			check: func(t *testing.T, fn function.Function, rw *httptest.ResponseRecorder) {
				require.Equal(t, http.StatusOK, rw.Code)
				require.Equal(t, "Hello, "+fn.Tenant, rw.Body.String())
			},
		},
		{
			name:        "controller.get arbitrary error",
			maxAttempts: 2,
			getErrs:     []error{fmt.Errorf("arbitrary error")},
			check: func(t *testing.T, fn function.Function, rw *httptest.ResponseRecorder) {
				require.Equal(t, http.StatusOK, rw.Code)
				require.Equal(t, "Hello, "+fn.Tenant, rw.Body.String())
			},
		},
		{
			name:          "round trip net op dial error",
			maxAttempts:   2,
			roundTripErrs: []error{&net.OpError{Op: "dial", Err: fmt.Errorf("arbitrary error")}},
			check: func(t *testing.T, fn function.Function, rw *httptest.ResponseRecorder) {
				require.Equal(t, http.StatusOK, rw.Code)
				require.Equal(t, "Hello, "+fn.Tenant, rw.Body.String())
			},
		},
		{
			name:          "round trip net op timeout error",
			maxAttempts:   2,
			roundTripErrs: []error{&net.OpError{Op: "foo", Err: timeoutError{}}},
			check: func(t *testing.T, fn function.Function, rw *httptest.ResponseRecorder) {
				require.Equal(t, http.StatusOK, rw.Code)
				require.Equal(t, "Hello, "+fn.Tenant, rw.Body.String())
			},
		},
		{
			name:        "controller.get and round trip errors",
			maxAttempts: 4,
			getErrs:     []error{fmt.Errorf("arbitrary error")},
			roundTripErrs: []error{
				&net.OpError{Op: "dial", Err: fmt.Errorf("arbitrary error")},
				&net.OpError{Op: "foo", Err: timeoutError{}},
			},
			check: func(t *testing.T, fn function.Function, rw *httptest.ResponseRecorder) {
				require.Equal(t, http.StatusOK, rw.Code)
				require.Equal(t, "Hello, "+fn.Tenant, rw.Body.String())
			},
		},
		{
			name:        "controller.get and round trip errors exceed max attempts",
			maxAttempts: 3,
			getErrs:     []error{fmt.Errorf("arbitrary error")},
			roundTripErrs: []error{
				&net.OpError{Op: "dial", Err: fmt.Errorf("arbitrary error")},
				&net.OpError{Op: "dial", Err: timeoutError{}},
			},
			check: func(t *testing.T, fn function.Function, rw *httptest.ResponseRecorder) {
				require.Equal(t, http.StatusBadGateway, rw.Code)
				require.Empty(t, rw.Body)
			},
		},
	}

	for _, tc := range testCases {
		fixture.SetFlag(t, &FlagMaxRoundTripAttempts, tc.maxAttempts)

		fn := fixture.NewFunction()

		getErrsIndex := 0
		mcc := fixture.NewMockControllerClient(t)
		mcc.HandleGet(fn, func(ctx context.Context, fn function.Function) (*function.Instance, error) {
			if len(tc.getErrs) > 0 && getErrsIndex < len(tc.getErrs) {
				getErrsIndex++
				return nil, tc.getErrs[getErrsIndex-1]
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
	}
}

type timeoutError struct{}

func (timeoutError) Error() string { return "timeout error" }

func (timeoutError) Timeout() bool { return true }

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
