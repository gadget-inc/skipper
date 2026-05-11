package fixture

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aidanwoods.dev/go-paseto"
	"github.com/coder/websocket"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/skipper"
	"gotest.tools/v3/assert"
)

// fixtureEnv bundles the inputs the fixture server needs to boot:
// a verified PASETO public key (also exposed in PEM form so callers can
// exercise the exact env-var path the K8s deployment uses), and a sibling
// secret key for minting tokens in tests.
type fixtureEnv struct {
	publicKey    paseto.V2AsymmetricPublicKey
	publicKeyPEM string
	secretKey    paseto.V2AsymmetricSecretKey
	tokenPath    string
}

func newFixtureEnv(t *testing.T) *fixtureEnv {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	assert.NilError(t, err)

	der, err := x509.MarshalPKIXPublicKey(pub)
	assert.NilError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})

	pk, err := paseto.NewV2AsymmetricPublicKeyFromBytes(pub)
	assert.NilError(t, err)
	sk, err := paseto.NewV2AsymmetricSecretKeyFromBytes(priv)
	assert.NilError(t, err)

	return &fixtureEnv{
		publicKey:    pk,
		publicKeyPEM: string(pemBytes),
		secretKey:    sk,
		tokenPath:    filepath.Join(t.TempDir(), "token"),
	}
}

func startFixtureServer(t *testing.T, env *fixtureEnv) *httptest.Server {
	t.Helper()

	srv, err := New(env.publicKey, env.tokenPath)
	assert.NilError(t, err)

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func (env *fixtureEnv) mintToken(t *testing.T, tenant string) string {
	t.Helper()
	tok := paseto.NewToken()
	tok.SetSubject(tenant)
	now := time.Now()
	tok.SetIssuedAt(now)
	tok.SetNotBefore(now)
	tok.SetExpiration(now.Add(time.Hour))
	return tok.V2Sign(env.secretKey)
}

func assignRequest(t *testing.T, baseURL, token, assignmentJSON string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+"/__skipper/assign", nil)
	assert.NilError(t, err)
	req.Header.Set(key.Token.Header, token)
	req.Header.Set(skipper.LegacyFunctionKey.Header, assignmentJSON)
	return req
}

func TestParsePublicKeyPEMAcceptsSPKI(t *testing.T) {
	t.Parallel()

	env := newFixtureEnv(t)
	parsed, err := ParsePublicKeyPEM([]byte(env.publicKeyPEM))
	assert.NilError(t, err)

	token := env.mintToken(t, "tenant-1")
	parser := paseto.NewParserForValidNow()
	_, err = parser.ParseV2Public(parsed, token)
	assert.NilError(t, err)
}

func TestParsePublicKeyPEMRejectsGarbage(t *testing.T) {
	t.Parallel()

	_, err := ParsePublicKeyPEM([]byte("not a pem block"))
	assert.ErrorContains(t, err, "no PEM block")
}

func TestHealthz(t *testing.T) {
	t.Parallel()

	env := newFixtureEnv(t)
	ts := startFixtureServer(t, env)

	resp, err := http.Get(ts.URL + "/healthz")
	assert.NilError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, resp.StatusCode, http.StatusOK)
}

func TestAssign(t *testing.T) {
	t.Parallel()

	env := newFixtureEnv(t)
	ts := startFixtureServer(t, env)

	token := env.mintToken(t, "tenant-1")
	req := assignRequest(t, ts.URL, token, `{"deployment":"d","tenant":"tenant-1"}`)

	resp, err := http.DefaultClient.Do(req)
	assert.NilError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, resp.StatusCode, http.StatusOK)

	persisted, err := os.ReadFile(env.tokenPath)
	assert.NilError(t, err)
	assert.Equal(t, string(persisted), token)
}

func TestReAssignReturns409(t *testing.T) {
	t.Parallel()

	env := newFixtureEnv(t)
	ts := startFixtureServer(t, env)

	token := env.mintToken(t, "tenant-1")
	first, err := http.DefaultClient.Do(assignRequest(t, ts.URL, token, `{}`))
	assert.NilError(t, err)
	first.Body.Close()
	assert.Equal(t, first.StatusCode, http.StatusOK)

	second, err := http.DefaultClient.Do(assignRequest(t, ts.URL, token, `{}`))
	assert.NilError(t, err)
	defer second.Body.Close()
	assert.Equal(t, second.StatusCode, http.StatusConflict)
}

func TestPreAssignReturns503(t *testing.T) {
	t.Parallel()

	env := newFixtureEnv(t)
	ts := startFixtureServer(t, env)

	resp, err := http.Get(ts.URL + "/anything")
	assert.NilError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, resp.StatusCode, http.StatusServiceUnavailable)
}

func TestWebSocketPingPong(t *testing.T) {
	t.Parallel()

	env := newFixtureEnv(t)
	ts := startFixtureServer(t, env)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	assert.NilError(t, err)
	defer conn.Close(websocket.StatusNormalClosure, "test done")

	assert.NilError(t, conn.Write(ctx, websocket.MessageText, []byte("ping")))

	mt, msg, err := conn.Read(ctx)
	assert.NilError(t, err)
	assert.Equal(t, mt, websocket.MessageText)
	assert.Equal(t, string(msg), "pong")
}

func TestEchoesAfterAssign(t *testing.T) {
	t.Parallel()

	env := newFixtureEnv(t)
	ts := startFixtureServer(t, env)

	token := env.mintToken(t, "tenant-1")
	assignResp, err := http.DefaultClient.Do(assignRequest(t, ts.URL, token, `{}`))
	assert.NilError(t, err)
	assignResp.Body.Close()
	assert.Equal(t, assignResp.StatusCode, http.StatusOK)

	body := strings.NewReader(`hello`)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/echo/path?q=1", body)
	assert.NilError(t, err)
	req.Header.Set("X-Custom", "custom-value")
	req.Header.Set("Content-Type", "text/plain")

	resp, err := http.DefaultClient.Do(req)
	assert.NilError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, resp.StatusCode, http.StatusOK)
	assert.Equal(t, resp.Header.Get("Content-Type"), "application/json")

	var got struct {
		Method  string            `json:"method"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}
	dec := json.NewDecoder(resp.Body)
	assert.NilError(t, dec.Decode(&got))
	assert.Equal(t, got.Method, http.MethodPost)
	assert.Equal(t, got.URL, "/echo/path?q=1")
	assert.Equal(t, got.Body, "hello")
	assert.Equal(t, got.Headers["X-Custom"], "custom-value")
}
