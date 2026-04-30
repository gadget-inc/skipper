package web

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("SKIPPER_PLAYWRIGHT") == "" {
		os.Exit(m.Run())
	}

	srv := multiSupServer()

	addr := "127.0.0.1:8077"
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to listen: %v\n", err)
		os.Exit(1)
	}

	go http.Serve(ln, srv.Handler()) //nolint:errcheck

	fmt.Fprintf(os.Stderr, "test server listening on http://%s\n", addr)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}
