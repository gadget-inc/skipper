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

func Init(ctx context.Context) func() {
	if !FlagPprof.Value() {
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
		Addr:    net.JoinHostPort(FlagPprofHost.Value(), strconv.Itoa(FlagPprofPort.Value())),
		Handler: mux,
	}

	go func() {
		log.Info(ctx, "serving pprof", key.Addr.Slog(server.Addr))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error(ctx, "failed to serve pprof", key.Error.Slog(err))
		}
	}()

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), FlagPprofShutdownTimeout.Value())
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Error(ctx, "failed to shutdown pprof", key.Error.Slog(err))
		}
	}
}
