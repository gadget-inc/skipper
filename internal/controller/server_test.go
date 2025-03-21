package controller

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/function"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/go-json-experiment/json"
	"github.com/shoenig/test/must"
	"k8s.io/client-go/kubernetes/fake"
)

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rw := httptest.NewRecorder()

	ctrl := New(nil, fake.NewClientset(), nil)
	ctrl.Handler().ServeHTTP(rw, req)

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
				must.Eq(t, 1, ctrl.routerHeartbeats.Size())
				heartbeat, ok := ctrl.routerHeartbeats.Load(heartbeats[0].Function)
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
				must.Eq(t, 2, ctrl.routerHeartbeats.Size())
				for _, hb := range heartbeats {
					heartbeat, ok := ctrl.routerHeartbeats.Load(hb.Function)
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
				heartbeatTimestamp := time.Now()
				ctrl.routerHeartbeats.Store(fn, RouterHeartbeats{fixture.RouterIP: {Timestamp: heartbeatTimestamp}})

				// send an old heartbeat
				return []function.Heartbeat{
					{Function: fn, Timestamp: heartbeatTimestamp.Add(-time.Hour)},
				}
			},
			check: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller, heartbeats []function.Heartbeat) {
				must.Eq(t, 1, ctrl.routerHeartbeats.Size())

				sentHeartbeat := heartbeats[0]
				keptHeartbeat, ok := ctrl.routerHeartbeats.Load(sentHeartbeat.Function)

				must.True(t, ok)
				must.NotEq(t, keptHeartbeat[fixture.RouterIP].Timestamp, sentHeartbeat.Timestamp)
				must.Eq(t, keptHeartbeat[fixture.RouterIP].Timestamp, sentHeartbeat.Timestamp.Add(time.Hour))
			},
		},
		{
			name: "forwards heartbeats",
			setup: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller) []function.Heartbeat {
				ctrl.ring.Add(fixture.ControllerIP)
				ctrl.ring.Add(fixture.ControllerIP2)
				hbs := []function.Heartbeat{{Function: fixture.NewFunction(), Timestamp: time.Now()}}

				mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []function.Heartbeat, forwardedFor ...string) error {
					must.Eq(t, hbs, heartbeats)
					must.Eq(t, []string{fixture.ControllerIP, fixture.ControllerIP2}, forwardedFor)
					return nil
				})

				return hbs
			},
			check: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller, heartbeats []function.Heartbeat) {
				// give the goroutine that forwards the heartbeats a chance to run
				time.Sleep(10 * time.Millisecond)
			},
		},
		{
			name: "garbage collects old heartbeats",
			setup: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller) []function.Heartbeat {
				// seed the controller with a old heartbeat from a different router
				fn := fixture.NewFunction()
				ctrl.routerHeartbeats.Store(fn, RouterHeartbeats{fixture.RouterIP2: {Timestamp: time.Now().Add(-(FlagHeartbeatTimeout.Value() + time.Second))}})

				return []function.Heartbeat{
					{Function: fn, Timestamp: time.Now()},
				}
			},
			check: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller, heartbeats []function.Heartbeat) {
				sentHeartbeat := heartbeats[0]
				keptHeartbeat, ok := ctrl.routerHeartbeats.Load(sentHeartbeat.Function)
				must.True(t, ok)
				must.Eq(t, 1, len(keptHeartbeat))
				must.MapNotContainsKey(t, keptHeartbeat, fixture.RouterIP2)
				must.Eq(t, sentHeartbeat.Timestamp, keptHeartbeat[fixture.RouterIP].Timestamp)
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
			ctrl.Handler().ServeHTTP(rw, req)

			must.Eq(t, http.StatusOK, rw.Code)
			must.Length(t, 0, rw.Body)
			tc.check(t, mcc, ctrl, heartbeats)
		})
	}
}
