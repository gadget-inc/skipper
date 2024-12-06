package router

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gadget-inc/fusion/internal/fixture"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/stretchr/testify/require"
)

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
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte("Hello, " + fn.Tenant))
		}), nil
	})

	rw := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	fn.SetHeaders(req)

	router := New(mockControllerClient)
	router.ServeHTTP(rw, req)

	require.Equal(t, http.StatusOK, rw.Code)
	require.Equal(t, "Hello, "+fn.Tenant, rw.Body.String())
}

func TestHeartbeats(t *testing.T) {
	fn := fixture.NewFunction()

	fixture.SetFlag(t, &FlagHeartbeatInterval, 1*time.Second)

	mockControllerClient := fixture.NewMockControllerClient(t)
	mockControllerClient.HandleGet(fn, func(ctx context.Context, fn function.Function) (*function.Instance, error) {
		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte("Hello, " + fn.Tenant))
		}), nil
	})

	rw := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	fn.SetHeaders(req)

	testStartTime := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	router := New(mockControllerClient)
	router.Start(ctx)
	router.ServeHTTP(rw, req)

	require.Equal(t, http.StatusOK, rw.Code)
	require.Equal(t, "Hello, "+fn.Tenant, rw.Body.String())

	require.Eventually(t, func() bool {
		return len(mockControllerClient.Heartbeats()) > 0
	}, 3*time.Second, time.Second)

	heartbeat := mockControllerClient.Heartbeats()[0]
	require.Equal(t, fn, heartbeat.Function)
	require.True(t, heartbeat.Timestamp.After(testStartTime))
}

type timeoutError struct{}

func (timeoutError) Error() string { return "timeout error" }

func (timeoutError) Timeout() bool { return true }

func TestRetries(t *testing.T) {
	fn := fixture.NewFunction()

	errs := []error{
		&net.OpError{Op: "dial", Err: fmt.Errorf("dial error")},
		&net.OpError{Op: "dial", Err: timeoutError{}},
		fmt.Errorf("arbitrary error"),
	}

	fixture.SetFlag(t, &FlagGetAttempts, len(errs)+1)

	attempt := 0

	mockControllerClient := fixture.NewMockControllerClient(t)
	mockControllerClient.HandleGet(fn, func(ctx context.Context, fn function.Function) (*function.Instance, error) {
		if attempt < len(errs) {
			attempt++
			return nil, errs[attempt-1]
		}

		return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte("Hello, " + fn.Tenant))
		}), nil
	})

	rw := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	fn.SetHeaders(req)

	router := New(mockControllerClient)
	router.ServeHTTP(rw, req)

	require.Equal(t, http.StatusOK, rw.Code)
	require.Equal(t, "Hello, "+fn.Tenant, rw.Body.String())
}

func TestTooManyRetries(t *testing.T) {
	fn := fixture.NewFunction()

	fixture.SetFlag(t, &FlagGetAttempts, 1)

	mockControllerClient := fixture.NewMockControllerClient(t)
	mockControllerClient.HandleGet(fn, func(ctx context.Context, fn function.Function) (*function.Instance, error) {
		return nil, fmt.Errorf("arbitrary error")
	})

	rw := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	fn.SetHeaders(req)

	router := New(mockControllerClient)
	router.ServeHTTP(rw, req)

	require.Equal(t, http.StatusBadGateway, rw.Code)
	require.Empty(t, rw.Body)
}
