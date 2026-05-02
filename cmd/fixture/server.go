package main

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"sync"

	"aidanwoods.dev/go-paseto"
	"github.com/coder/websocket"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/skipper"
)

// server is the in-cluster fixture HTTP+WebSocket service. A pod hosts
// exactly one server; once /__skipper/assign succeeds it commits to that
// assignment for its lifetime, and the controller terminates the pod
// when releasing it.
type server struct {
	publicKey paseto.V2AsymmetricPublicKey
	tokenPath string

	mu       sync.Mutex
	assigned bool
}

// newServer constructs a server. If tokenPath already exists on disk the
// server boots in the assigned state — pods that restart after a successful
// assign keep their identity.
func newServer(publicKey paseto.V2AsymmetricPublicKey, tokenPath string) (*server, error) {
	s := &server{publicKey: publicKey, tokenPath: tokenPath}
	if _, err := os.Stat(tokenPath); err == nil {
		s.assigned = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return s, nil
}

func (s *server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/__skipper/assign", s.handleAssign)
	mux.HandleFunc("/", s.handleEcho)
	return mux
}

func (s *server) handleEcho(w http.ResponseWriter, r *http.Request) {
	if isWebSocketUpgrade(r) {
		s.handleWebSocket(w, r)
		return
	}

	s.mu.Lock()
	assigned := s.assigned
	s.mu.Unlock()

	if !assigned {
		http.Error(w, "not assigned", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Error(r.Context(), "failed to read request body", key.Error.Slog(err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	if err := enc.Encode(struct {
		Method  string      `json:"method"`
		URL     string      `json:"url"`
		Headers http.Header `json:"headers"`
		Body    string      `json:"body"`
	}{
		Method:  r.Method,
		URL:     r.URL.RequestURI(),
		Headers: r.Header,
		Body:    string(body),
	}); err != nil {
		log.Error(r.Context(), "failed to encode echo response", key.Error.Slog(err))
	}
}

func isWebSocketUpgrade(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Connection"), "upgrade") &&
		!hasToken(r.Header.Values("Connection"), "upgrade") {
		return false
	}
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func hasToken(values []string, target string) bool {
	for _, v := range values {
		for _, tok := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(tok), target) {
				return true
			}
		}
	}
	return false
}

func (s *server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Warn(r.Context(), "websocket accept failed", key.Error.Slog(err))
		return
	}
	defer func() { _ = conn.CloseNow() }()

	ctx := r.Context()
	for {
		mt, msg, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if mt == websocket.MessageText && string(msg) == "ping" {
			if err := conn.Write(ctx, websocket.MessageText, []byte("pong")); err != nil {
				return
			}
		}
	}
}

func (s *server) handleAssign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.assigned {
		http.Error(w, "already assigned", http.StatusConflict)
		return
	}

	token := r.Header.Get(key.Token.Header)
	if token == "" {
		http.Error(w, "missing "+key.Token.Header, http.StatusBadRequest)
		return
	}
	if r.Header.Get(skipper.FunctionKey.Header) == "" {
		http.Error(w, "missing "+skipper.FunctionKey.Header, http.StatusBadRequest)
		return
	}

	parser := paseto.NewParserForValidNow()
	if _, err := parser.ParseV2Public(s.publicKey, token); err != nil {
		log.Warn(r.Context(), "paseto verification failed", key.Error.Slog(err))
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	if err := os.WriteFile(s.tokenPath, []byte(token), 0o600); err != nil {
		log.Error(r.Context(), "failed to persist token", key.Error.Slog(err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.assigned = true
	log.Info(r.Context(), "assigned")
	w.WriteHeader(http.StatusOK)
}
