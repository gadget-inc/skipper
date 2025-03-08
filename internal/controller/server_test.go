package controller

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gadget-inc/fusion/internal/fixture"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/goccy/go-json"
	"github.com/shoenig/test/must"
	"k8s.io/client-go/kubernetes/fake"
)

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rw := httptest.NewRecorder()

	ctrl := New(nil, fake.NewClientset(), nil)
	ctrl.ServeHTTP(rw, req)

	must.Eq(t, http.StatusOK, rw.Code)
	must.Length(t, 0, rw.Body)
}

func TestHandleHeartbeat(t *testing.T) {
	testCases := []struct {
		name  string
		setup func(*testing.T, *fixture.MockControllerClient, *Controller) []function.Heartbeat
		check func(*testing.T, *fixture.MockControllerClient, *Controller, []function.Heartbeat)
	}{
		{
			name: "one",
			setup: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller) []function.Heartbeat {
				return []function.Heartbeat{
					{Function: fixture.NewFunction(), Timestamp: time.Now()},
				}
			},
			check: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller, heartbeats []function.Heartbeat) {
				must.Eq(t, 1, ctrl.heartbeats.Size())
				heartbeat, ok := ctrl.heartbeats.Load(heartbeats[0].Function)
				must.True(t, ok)
				must.Eq(t, heartbeats[0].Timestamp, heartbeat[fixture.RouterIP].Timestamp)
			},
		},
		{
			name: "multiple",
			setup: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller) []function.Heartbeat {
				return []function.Heartbeat{
					{Function: fixture.NewFunction(), Timestamp: time.Now()},
					{Function: fixture.NewFunction(), Timestamp: time.Now()},
				}
			},
			check: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller, heartbeats []function.Heartbeat) {
				must.Eq(t, 2, ctrl.heartbeats.Size())
				for _, hb := range heartbeats {
					heartbeat, ok := ctrl.heartbeats.Load(hb.Function)
					must.True(t, ok)
					must.Eq(t, hb.Timestamp, heartbeat[fixture.RouterIP].Timestamp)
				}
			},
		},
		{
			name: "keeps most recent",
			setup: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller) []function.Heartbeat {
				// seed the controller with a recent heartbeat
				fn := fixture.NewFunction()
				heartbeat := time.Now()
				ctrl.heartbeats.Store(fn, RouterHeartbeats{fixture.RouterIP: {Timestamp: heartbeat}})

				// send an old heartbeat
				return []function.Heartbeat{
					{Function: fn, Timestamp: heartbeat.Add(-time.Hour)},
				}
			},
			check: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller, heartbeats []function.Heartbeat) {
				must.Eq(t, 1, ctrl.heartbeats.Size())

				sentTimestamp := heartbeats[0].Timestamp
				keptTimestamp, ok := ctrl.heartbeats.Load(heartbeats[0].Function)

				must.True(t, ok)
				must.NotEq(t, keptTimestamp[fixture.RouterIP].Timestamp, sentTimestamp)
				must.Eq(t, keptTimestamp[fixture.RouterIP].Timestamp, sentTimestamp.Add(time.Hour))
			},
		},
		{
			name: "forwards heartbeats",
			setup: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller) []function.Heartbeat {
				ctrl.ring.Add("127.0.0.2")
				hbs := []function.Heartbeat{{Function: fixture.NewFunction(), Timestamp: time.Now()}}

				mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []function.Heartbeat, forwardedFor ...string) error {
					must.Eq(t, hbs, heartbeats)
					must.Eq(t, []string{fixture.ControllerIP}, forwardedFor)
					return nil
				})

				return hbs
			},
			check: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller, heartbeats []function.Heartbeat) {
				// give the goroutine that forwards the heartbeats a chance to run
				time.Sleep(10 * time.Millisecond)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mcc := fixture.NewMockControllerClient(t)
			ctrl := New(func(host string, port int) Client { return mcc }, fake.NewClientset(), nil)

			heartbeats := tc.setup(t, mcc, ctrl)
			heartbeatBytes, err := json.Marshal(heartbeats)
			must.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/heartbeat", bytes.NewReader(heartbeatBytes))
			req.Header.Set(key.RouterIP.Header, fixture.RouterIP)
			rw := httptest.NewRecorder()
			ctrl.ServeHTTP(rw, req)

			must.Eq(t, http.StatusOK, rw.Code)
			must.Length(t, 0, rw.Body)
			tc.check(t, mcc, ctrl, heartbeats)
		})
	}
}
