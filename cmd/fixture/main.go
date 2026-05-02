// Command fixture is the in-cluster HTTP+WebSocket service skipper assigns
// pods to during integration testing. The container reads its PASETO public
// key from SKIPPER_PUBLIC_KEY (PEM-encoded SPKI ed25519), persists the
// accepted token at /tmp/token, and exports OpenTelemetry traces and metrics
// over OTLP when the standard OTEL_* environment variables are set.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"aidanwoods.dev/go-paseto"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	defaultPort      = 8888
	defaultTokenPath = "/tmp/token"
	publicKeyEnv     = "SKIPPER_PUBLIC_KEY"
	portEnv          = "SKIPPER_FIXTURE_PORT"
	tokenPathEnv     = "SKIPPER_FIXTURE_TOKEN_PATH"
	shutdownTimeout  = 10 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	log.Init(&log.Config{Format: "json"})

	pemBytes := os.Getenv(publicKeyEnv)
	if pemBytes == "" {
		return fmt.Errorf("%s is not set", publicKeyEnv)
	}
	publicKey, err := parsePublicKeyPEM([]byte(pemBytes))
	if err != nil {
		return fmt.Errorf("parsing %s: %w", publicKeyEnv, err)
	}

	tokenPath := os.Getenv(tokenPathEnv)
	if tokenPath == "" {
		tokenPath = defaultTokenPath
	}

	port := defaultPort
	if v := os.Getenv(portEnv); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid %s: %w", portEnv, err)
		}
		port = p
	}

	srv, err := newServer(publicKey, tokenPath)
	if err != nil {
		return fmt.Errorf("constructing server: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTelemetry := initTelemetry(ctx)
	defer func() {
		if err := shutdownTelemetry(context.Background()); err != nil {
			log.Warn(context.Background(), "telemetry shutdown error", key.Error.Slog(err))
		}
	}()

	handler := otelhttp.NewHandler(srv.Handler(), "fixture")
	httpServer := &http.Server{
		Addr:    net.JoinHostPort("", strconv.Itoa(port)),
		Handler: handler,
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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Warn(shutdownCtx, "fixture shutdown error", key.Error.Slog(err))
	}
	return nil
}

func parsePublicKeyPEM(pemBytes []byte) (paseto.V2AsymmetricPublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return paseto.V2AsymmetricPublicKey{}, errors.New("no PEM block found")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return paseto.V2AsymmetricPublicKey{}, fmt.Errorf("parsing PKIX public key: %w", err)
	}
	ed, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return paseto.V2AsymmetricPublicKey{}, fmt.Errorf("expected ed25519 public key, got %T", parsed)
	}
	return paseto.NewV2AsymmetricPublicKeyFromBytes(ed)
}
