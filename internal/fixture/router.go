package fixture

import "os"

const (
	RouterIP  = "127.0.1.0"
	RouterIP2 = "127.0.1.1"
)

// RouterIntegrationURL returns the base URL for integration tests.
// Override with SKIPPER_ROUTER_URL; defaults to the local dev server.
func RouterIntegrationURL() string {
	if u := os.Getenv("SKIPPER_ROUTER_URL"); u != "" {
		return u
	}
	return "http://127.0.0.1:8081"
}
