// Package fixture is the in-cluster HTTP+WebSocket service skipper assigns
// pods to during integration testing. It exposes the assignment endpoint,
// the echo handler, and a small ed25519/PASETO public-key parser used to
// verify assignment tokens.
package fixture

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
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

// Server is the in-cluster fixture HTTP+WebSocket service. A pod hosts
// exactly one Server; once /__skipper/assign succeeds it commits to that
// assignment for its lifetime, and the controller terminates the pod
// when releasing it.
type Server struct {
	publicKey paseto.V2AsymmetricPublicKey
	tokenPath string

	mu       sync.Mutex
	assigned bool
}

// New constructs a Server. If tokenPath already exists on disk the Server
// boots in the assigned state -- pods that restart after a successful
// assign keep their identity.
func New(publicKey paseto.V2AsymmetricPublicKey, tokenPath string) (*Server, error) {
	s := &Server{publicKey: publicKey, tokenPath: tokenPath}
	if _, err := os.Stat(tokenPath); err == nil {
		s.assigned = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return s, nil
}

// Handler returns the HTTP handler tree the fixture exposes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/__skipper/assign", s.handleAssign)
	mux.HandleFunc("/", s.handleEcho)
	return mux
}

func (s *Server) handleEcho(w http.ResponseWriter, r *http.Request) {
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

	// Match the legacy TS fixture's flat-header shape: each header maps
	// to a single comma-joined string. internal/fixture.FixtureResponse
	// reverses this with strings.Split on `,`. Go's net/http promotes
	// Host out of r.Header and into r.Host, so re-attach it explicitly
	// for parity with Node's request.headers shape.
	headers := make(map[string]string, len(r.Header)+1)
	for k, v := range r.Header {
		headers[k] = strings.Join(v, ", ")
	}
	if r.Host != "" {
		headers["Host"] = r.Host
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	if err := enc.Encode(struct {
		Method  string            `json:"method"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}{
		Method:  r.Method,
		URL:     r.URL.RequestURI(),
		Headers: headers,
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
		for tok := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(strings.TrimSpace(tok), target) {
				return true
			}
		}
	}
	return false
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) handleAssign(w http.ResponseWriter, r *http.Request) {
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
	if r.Header.Get(skipper.AssignmentKey.Header) == "" && r.Header.Get(skipper.LegacyFunctionKey.Header) == "" {
		http.Error(w, "missing "+skipper.LegacyFunctionKey.Header, http.StatusBadRequest)
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

// ParsePublicKeyPEM decodes an SPKI-encoded ed25519 public key from PEM
// bytes and returns the corresponding PASETO V2 asymmetric public key.
func ParsePublicKeyPEM(pemBytes []byte) (paseto.V2AsymmetricPublicKey, error) {
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
