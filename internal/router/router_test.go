package router

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gadget-inc/fusion/internal/fixture"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/google/uuid"
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
	testServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
		fmt.Fprintf(rw, "Hello, %s", fn.Tenant)
	}))
	defer testServer.Close()

	mockControllerClient := fixture.NewMockControllerClient(t)
	mockControllerClient.HandleGet(fn, func(ctx context.Context, fn function.Function) (*function.Instance, error) {
		return &function.Instance{
			Function:   fn,
			Name:       uuid.NewString(),
			Addr:       testServer.Listener.Addr().String(),
			Version:    uuid.NewString(),
			AssignedAt: time.Now(),
			ReadyAt:    time.Now(),
		}, nil
	})

	rw := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	fn.SetHeaders(req)

	router := New(mockControllerClient)
	router.ServeHTTP(rw, req)

	require.Equal(t, http.StatusOK, rw.Code)
	require.Equal(t, "Hello, "+fn.Tenant, rw.Body.String())
}

func TestControllerGetRetries(t *testing.T) {
	fn := fixture.NewFunction()
	testServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
		fmt.Fprintf(rw, "Hello, %s", fn.Tenant)
	}))
	defer testServer.Close()

	attempts := 0

	mockControllerClient := fixture.NewMockControllerClient(t)
	mockControllerClient.HandleGet(fn, func(ctx context.Context, fn function.Function) (*function.Instance, error) {
		if attempts < 2 {
			attempts++
			return nil, fmt.Errorf("controller get error")
		}

		return &function.Instance{
			Function:   fn,
			Name:       uuid.NewString(),
			Addr:       testServer.Listener.Addr().String(),
			Version:    uuid.NewString(),
			AssignedAt: time.Now(),
			ReadyAt:    time.Now(),
		}, nil
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testStartTime := time.Now()
	router := New(mockControllerClient)
	router.Start(ctx)
	router.ServeHTTP(rw, req)

	require.Equal(t, http.StatusOK, rw.Code)
	require.Equal(t, "Hello, "+fn.Tenant, rw.Body.String())

	require.Eventually(t, func() bool {
		return len(mockControllerClient.Heartbeats()) > 0
	}, 6*time.Second, time.Second)

	heartbeat := mockControllerClient.Heartbeats()[0]
	require.Equal(t, fn, heartbeat.Function)
	require.True(t, heartbeat.Timestamp.After(testStartTime))
}
