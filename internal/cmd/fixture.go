package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gadget-inc/skipper/internal/cmd/fixture"
	"github.com/gadget-inc/skipper/internal/config"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const fixtureShutdownTimeout = 10 * time.Second

// fixtureConfig holds the fixture binary's CLI configuration. Flag names
// are chosen so internal/config derives the historical SKIPPER_PUBLIC_KEY,
// SKIPPER_FIXTURE_PORT, SKIPPER_FIXTURE_TOKEN_PATH env vars unchanged.
type fixtureConfig struct {
	PublicKey string `flag:"public-key" description:"PEM-encoded ed25519 public key used to verify assignment tokens." required:"true" sensitive:"true"`
	Port      int    `flag:"fixture-port" description:"Port the fixture HTTP server listens on." default:"8888"`
	TokenPath string `flag:"fixture-token-path" description:"Path where the assigned token is persisted." default:"/tmp/token"`
}

// NewFixture creates the fixture command.
// Optional deps can be provided for testing; if nil, production defaults are used.
func NewFixture(deps *FixtureDeps) *cobra.Command {
	if deps == nil {
		deps = DefaultFixtureDeps()
	}
	cfg := config.New[fixtureConfig]()
	return Build(Spec{
		Use:     "fixture",
		Short:   "Run the in-cluster HTTP+WebSocket test fixture",
		Configs: []any{cfg},
		Base:    true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runFixture(cmd.Context(), cfg, deps)
		},
	})
}

func runFixture(ctx context.Context, cfg *fixtureConfig, deps *FixtureDeps) error {
	publicKey, err := deps.LoadPublicKey([]byte(cfg.PublicKey))
	if err != nil {
		return fmt.Errorf("parsing public key: %w", err)
	}

	tokenPath := deps.ResolveTokenPath(cfg.TokenPath)

	srv, err := fixture.New(publicKey, tokenPath)
	if err != nil {
		return fmt.Errorf("constructing server: %w", err)
	}

	httpServer := &http.Server{
		Addr:    net.JoinHostPort("", strconv.Itoa(cfg.Port)),
		Handler: otelhttp.NewHandler(srv.Handler(), "fixture"),
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Info(ctx, "fixture listening", key.Addr.Slog(httpServer.Addr))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("fixture server failed: %w", err)
	case <-ctx.Done():
		log.Info(ctx, "shutting down fixture")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), fixtureShutdownTimeout)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Warn(shutdownCtx, "fixture shutdown error", key.Error.Slog(err))
	}
	return nil
}
