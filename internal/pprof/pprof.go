package pprof

import (
	"context"
	"net"
	"net/http"
	"net/http/pprof"
	"strconv"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
)

func Init(ctx context.Context, cfg *Config) func() {
	if !cfg.Enabled {
		log.Info(ctx, "pprof disabled")
		return func() {}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)

	server := &http.Server{
		Addr:    net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Handler: mux,
	}

	go func() {
		log.Info(ctx, "serving pprof", key.Addr.Slog(server.Addr))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error(ctx, "failed to serve pprof", key.Error.Slog(err))
		}
	}()

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Error(ctx, "failed to shutdown pprof", key.Error.Slog(err))
		}
	}
}
