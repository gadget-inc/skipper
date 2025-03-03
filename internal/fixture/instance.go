package fixture

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/google/uuid"
)

func NewInstance(t *testing.T, fn function.Function, handler http.HandlerFunc) *function.Instance {
	testServer := httptest.NewServer(handler)
	t.Cleanup(testServer.Close)

	replicaSetCounter.Add(1)

	return &function.Instance{
		Function:   fn,
		Name:       uuid.NewString(),
		Addr:       testServer.Listener.Addr().String(),
		ReplicaSet: fn.Deployment + "-replicaset-" + strconv.Itoa(int(replicaSetCounter.Load())),
		AssignedAt: time.Now(),
		ReadyAt:    time.Now(),
	}
}
