package router

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gadget-inc/fusion/internal/fixture"
	"github.com/stretchr/testify/require"
)

func TestHealthz(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	f := fixture.New(t)
	res, err := f.SendRouterRequest(ctx, http.MethodGet, "/healthz", nil)
	require.NoError(t, err, "failed to send router healthz request")
	require.Equal(t, http.StatusOK, res.StatusCode, "unexpected status code")
}
