package router

import (
	"net"
	"net/http"
	"time"
)

// HTTPTransportSettings holds the operator-facing tuning of the
// router's outbound HTTP transport. The struct is the single source
// of truth: [New] constructs the live [http.Transport] from
// [DefaultHTTPTransportSettings], and the docs site reflects over
// the same struct to render its reference table. Adding a field
// here means it shows up in the table; flipping a bool means the
// table's prose changes.
//
// Fields that are not operator-facing (e.g. ExpectContinueTimeout,
// which is an HTTP/1.1 implementation detail) intentionally stay
// inline in [New] rather than joining this struct.
type HTTPTransportSettings struct {
	// DialTimeout caps how long Dial waits before failing the
	// connection attempt.
	DialTimeout time.Duration
	// KeepAlive is the TCP keep-alive interval applied to dialed
	// connections.
	KeepAlive time.Duration
	// MaxIdleConns is the maximum number of idle connections kept
	// across all hosts.
	MaxIdleConns int
	// IdleConnTimeout is how long an idle connection is retained
	// before being closed.
	IdleConnTimeout time.Duration
	// TLSHandshakeTimeout caps the TLS handshake duration.
	TLSHandshakeTimeout time.Duration
	// ForceAttemptHTTP2 controls whether the transport upgrades
	// connections to HTTP/2 when the peer supports it.
	ForceAttemptHTTP2 bool
	// DisableCompression suppresses the implicit Accept-Encoding
	// header so backends do not gzip responses for the router.
	DisableCompression bool
}

// DefaultHTTPTransportSettings is the production tuning installed by
// [New]. These values are also the source rendered into the docs
// site's transport table.
var DefaultHTTPTransportSettings = HTTPTransportSettings{
	DialTimeout:         2 * time.Second,
	KeepAlive:           30 * time.Second,
	MaxIdleConns:        100,
	IdleConnTimeout:     90 * time.Second,
	TLSHandshakeTimeout: 10 * time.Second,
	ForceAttemptHTTP2:   true,
	DisableCompression:  true,
}

// newHTTPTransport builds the live [http.Transport] from settings.
// ExpectContinueTimeout is set inline here because it is an
// HTTP/1.1 implementation detail (`100-continue` handshake) that
// operators do not tune; surfacing it through
// [HTTPTransportSettings] would force a row in the doc table
// without giving operators a knob worth turning.
func newHTTPTransport(s HTTPTransportSettings) *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   s.DialTimeout,
			KeepAlive: s.KeepAlive,
		}).DialContext,
		ForceAttemptHTTP2:     s.ForceAttemptHTTP2,
		MaxIdleConns:          s.MaxIdleConns,
		IdleConnTimeout:       s.IdleConnTimeout,
		TLSHandshakeTimeout:   s.TLSHandshakeTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:    s.DisableCompression,
	}
}
