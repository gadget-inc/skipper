package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gadget-inc/fusion/internal/fixture"
	"github.com/gadget-inc/fusion/internal/function"
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
		setup func(*testing.T, *Controller) []function.Heartbeat
		check func(*testing.T, *Controller, []function.Heartbeat)
	}{
		{
			name: "one",
			setup: func(t *testing.T, ctrl *Controller) []function.Heartbeat {
				return []function.Heartbeat{
					{Function: fixture.NewFunction(), Timestamp: time.Now()},
				}
			},
			check: func(t *testing.T, ctrl *Controller, heartbeats []function.Heartbeat) {
				must.Eq(t, 1, len(ctrl.heartbeats))
				must.Eq(t, heartbeats[0].Timestamp, ctrl.heartbeats[heartbeats[0].Function])
			},
		},
		{
			name: "multiple",
			setup: func(t *testing.T, ctrl *Controller) []function.Heartbeat {
				return []function.Heartbeat{
					{Function: fixture.NewFunction(), Timestamp: time.Now()},
					{Function: fixture.NewFunction(), Timestamp: time.Now()},
				}
			},
			check: func(t *testing.T, ctrl *Controller, heartbeats []function.Heartbeat) {
				must.Eq(t, 2, len(ctrl.heartbeats))
				for _, hb := range heartbeats {
					must.Eq(t, hb.Timestamp, ctrl.heartbeats[hb.Function])
				}
			},
		},
		{
			name: "old",
			setup: func(t *testing.T, ctrl *Controller) []function.Heartbeat {
				// seed the controller with a recent heartbeat
				fn := fixture.NewFunction()
				ctrl.heartbeats[fn] = time.Now()

				// send an old heartbeat
				return []function.Heartbeat{
					{Function: fn, Timestamp: time.Now().Add(-time.Hour)},
				}
			},
			check: func(t *testing.T, ctrl *Controller, heartbeats []function.Heartbeat) {
				must.Eq(t, 1, len(ctrl.heartbeats))

				sentTimestamp := heartbeats[0].Timestamp
				recordedTimestamp := ctrl.heartbeats[heartbeats[0].Function]

				must.NotEq(t, sentTimestamp, recordedTimestamp)
				must.True(t, recordedTimestamp.After(sentTimestamp))
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := New(nil, fake.NewClientset(), nil)

			heartbeats := tc.setup(t, ctrl)
			heartbeatBytes, err := json.Marshal(heartbeats)
			must.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/heartbeat", bytes.NewReader(heartbeatBytes))
			rw := httptest.NewRecorder()
			ctrl.ServeHTTP(rw, req)

			must.Eq(t, http.StatusOK, rw.Code)
			must.Length(t, 0, rw.Body)

			ctrl.heartbeatsMu.Lock()
			defer ctrl.heartbeatsMu.Unlock()
			tc.check(t, ctrl, heartbeats)
		})
	}
}
