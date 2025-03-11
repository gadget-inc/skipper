package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/function"
	"github.com/shoenig/test/must"
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

	must.Eq(t, http.StatusOK, rw.Code)
	must.Length(t, 0, rw.Body)
}

func TestSimple(t *testing.T) {
	fn := fixture.NewFunction()

	mockControllerClient := fixture.NewMockControllerClient(t)
	mockControllerClient.HandleInstance(func(ctx context.Context, fn function.Function) (*function.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			must.Eq(t, req.Method, http.MethodGet)
			must.Eq(t, req.URL.Path, "/")

			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte("Hello, " + fn.Tenant))
		}), nil
	})

	rw := httptest.NewRecorder()
	req := fixture.NewFunctionRequest(t, fn, http.MethodGet, "/", nil)

	router := New(mockControllerClient)
	router.ServeHTTP(rw, req)

	must.Eq(t, http.StatusOK, rw.Code)
	must.Eq(t, "Hello, "+fn.Tenant, rw.Body.String())
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
			mcc.HandleInstance(func(ctx context.Context, fn function.Function) (*function.Instance, error) {
				return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
					must.Eq(t, req.Method, tc.method)
				}), nil
			})

			rw := httptest.NewRecorder()
			req := fixture.NewFunctionRequest(t, fn, tc.method, "/", nil)

			router := New(mcc)
			router.ServeHTTP(rw, req)

			must.Eq(t, http.StatusOK, rw.Code)
		})

		// integration tests
		t.Run(tc.method+" integration", func(t *testing.T) {
			fn := fixture.NewEchoFunction()
			req := fixture.NewFunctionRequest(t, fn, tc.method, fixture.RouterIntegrationURL, nil)

			res, err := http.DefaultTransport.RoundTrip(req)
			must.NoError(t, err)
			defer res.Body.Close()
			must.Eq(t, http.StatusOK, res.StatusCode)

			echoResponse, err := fixture.ParseEchoResponse(res)
			must.NoError(t, err)
			must.Eq(t, tc.method, echoResponse.Method)
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

				must.Eq(t, []string{expectedHost}, headers.Values("Host"))
				must.SliceLen(t, 1, headers.Values("X-Forwarded-For")) // TODO: check the actual value
				must.Eq(t, []string{expectedHost}, headers.Values("X-Forwarded-Host"))
				must.Eq(t, []string{expectedProto}, headers.Values("X-Forwarded-Proto"))
				must.Eq(t, []string{fmt.Sprintf("for=%s;host=%s;proto=%s", headers.Get("X-Forwarded-For"), expectedHost, expectedProto)}, headers.Values("Forwarded"))
				must.MapLen(t, 5, headers)
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
				must.Eq(t, []string{"custom-value"}, headers.Values("X-Custom-Header"))
				must.Eq(t, []string{"multi-value-1", "multi-value-2"}, headers.Values("X-Custom-Multi-Header"))
				must.MapLen(t, 7, headers)
			},
		},
	}

	for _, tc := range testCases {
		// unit tests
		t.Run(tc.name, func(t *testing.T) {
			fn := fixture.NewFunction()

			mcc := fixture.NewMockControllerClient(t)
			mcc.HandleInstance(func(ctx context.Context, fn function.Function) (*function.Instance, error) {
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

			must.Eq(t, http.StatusOK, rw.Code)
			must.Length(t, 0, rw.Body)
		})

		// integration tests
		t.Run(tc.name+" integration", func(t *testing.T) {
			fn := fixture.NewEchoFunction()
			req := fixture.NewFunctionRequest(t, fn, http.MethodGet, fixture.RouterIntegrationURL, nil)
			req.Header.Set("User-Agent", "") // disable the default User-Agent header
			tc.setHeaders(fn, req)

			transport := &http.Transport{DisableCompression: true} // disable the default "Accept-Encoding: gzip" header
			res, err := transport.RoundTrip(req)
			must.NoError(t, err)
			defer res.Body.Close()
			must.Eq(t, http.StatusOK, res.StatusCode)

			echoResponse, err := fixture.ParseEchoResponse(res)
			must.NoError(t, err)

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
				must.Zero(t, len(body))
			},
		},
		{
			name: "text",
			getBody: func() (string, io.Reader) {
				return "text/plain", strings.NewReader("hello, world!")
			},
			checkBody: func(t *testing.T, body string) {
				must.Eq(t, "hello, world!", body)
			},
		},
		{
			name: "json",
			getBody: func() (string, io.Reader) {
				return "application/json", strings.NewReader(`{"key":"value"}`)
			},
			checkBody: func(t *testing.T, body string) {
				must.Eq(t, `{"key":"value"}`, body)
			},
		},
	}

	fn := fixture.NewFunction()

	for _, tc := range testCases {
		// unit tests
		t.Run(tc.name, func(t *testing.T) {
			mcc := fixture.NewMockControllerClient(t)
			mcc.HandleInstance(func(ctx context.Context, fn function.Function) (*function.Instance, error) {
				return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
					content, err := io.ReadAll(req.Body)
					must.NoError(t, err)
					tc.checkBody(t, string(content))
				}), nil
			})

			rw := httptest.NewRecorder()

			contentType, body := tc.getBody()
			req := fixture.NewFunctionRequest(t, fn, http.MethodPost, "/", body)
			req.Header.Set("Content-Type", contentType)

			router := New(mcc)
			router.ServeHTTP(rw, req)

			must.Eq(t, http.StatusOK, rw.Code)
		})

		// integration tests
		t.Run(tc.name+" integration", func(t *testing.T) {
			fn := fixture.NewEchoFunction()

			contentType, body := tc.getBody()
			req := fixture.NewFunctionRequest(t, fn, http.MethodPost, fixture.RouterIntegrationURL, body)
			req.Header.Set("Content-Type", contentType)

			res, err := http.DefaultTransport.RoundTrip(req)
			must.NoError(t, err)
			defer res.Body.Close()
			must.Eq(t, http.StatusOK, res.StatusCode)

			echoResponse, err := fixture.ParseEchoResponse(res)
			must.NoError(t, err)

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
	mcc.HandleInstance(func(ctx context.Context, fn function.Function) (*function.Instance, error) {
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

		must.Eq(t, fixture.RouterIP, routerIP)
		must.Eq(t, 1, len(heartbeats))
		must.Len(t, 0, forwardedFor)

		heartbeat := heartbeats[0]
		must.Eq(t, fn, heartbeat.Function)
		must.True(t, heartbeat.Timestamp.After(testStartTime))
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

	must.Eq(t, http.StatusOK, rw.Code)
	must.Eq(t, "Hello, "+fn.Tenant, rw.Body.String())

	heartbeat, ok := router.heartbeats.Load(fn)
	must.True(t, ok)
	must.Eq(t, fn, heartbeat.Function)
	must.True(t, heartbeat.Timestamp.After(testStartTime))
	must.Eq(t, 0, heartbeat.InFlightRequests) // ensure the number of in-flight requests is 0 now that the request is complete
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
				must.Eq(t, http.StatusOK, rw.Code)
				must.Eq(t, "Hello, "+fn.Tenant, rw.Body.String())
			},
		},
		{
			name:        "controller.get arbitrary error",
			maxAttempts: 2,
			getErrs:     []error{errors.New("arbitrary error")},
			check: func(t *testing.T, fn function.Function, rw *httptest.ResponseRecorder) {
				must.Eq(t, http.StatusOK, rw.Code)
				must.Eq(t, "Hello, "+fn.Tenant, rw.Body.String())
			},
		},
		{
			name:          "round trip dial error",
			maxAttempts:   2,
			roundTripErrs: []error{&net.OpError{Op: "dial", Err: errors.New("arbitrary error")}},
			check: func(t *testing.T, fn function.Function, rw *httptest.ResponseRecorder) {
				must.Eq(t, http.StatusOK, rw.Code)
				must.Eq(t, "Hello, "+fn.Tenant, rw.Body.String())
			},
		},
		{
			name:        "controller.get and round trip errors",
			maxAttempts: 4,
			getErrs:     []error{errors.New("arbitrary error")},
			roundTripErrs: []error{
				&net.OpError{Op: "dial", Err: errors.New("arbitrary error")},
			},
			check: func(t *testing.T, fn function.Function, rw *httptest.ResponseRecorder) {
				must.Eq(t, http.StatusOK, rw.Code)
				must.Eq(t, "Hello, "+fn.Tenant, rw.Body.String())
			},
		},
		{
			name:          "controller.get and round trip errors exceed max attempts",
			maxAttempts:   2,
			getErrs:       []error{errors.New("arbitrary error")},
			roundTripErrs: []error{&net.OpError{Op: "dial", Err: errors.New("arbitrary error")}},
			check: func(t *testing.T, fn function.Function, rw *httptest.ResponseRecorder) {
				must.Eq(t, http.StatusBadGateway, rw.Code)
				must.Length(t, 0, rw.Body)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fixture.SetFlag(t, &FlagMaxRoundTripAttempts, tc.maxAttempts)

			fn := fixture.NewFunction()

			getErrsIndex := 0
			mcc := fixture.NewMockControllerClient(t)
			mcc.HandleInstance(func(ctx context.Context, fn function.Function) (*function.Instance, error) {
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
		})
	}
}

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
