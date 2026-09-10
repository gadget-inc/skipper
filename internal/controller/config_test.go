package controller

import (
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/config"
	"gotest.tools/v3/assert"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		modify  func(*Config)
		wantErr string
	}{
		{
			name:   "valid config",
			modify: func(c *Config) {},
		},
		{
			name: "max concurrent stale replacements is 0",
			modify: func(c *Config) {
				c.MaxConcurrentStaleReplacements = 0
			},
			wantErr: "max concurrent stale replacements must be at least 1",
		},
		{
			name: "max concurrent stale replacements is negative",
			modify: func(c *Config) {
				c.MaxConcurrentStaleReplacements = -1
			},
			wantErr: "max concurrent stale replacements must be at least 1",
		},
		{
			name: "max concurrent stale replacements is 1",
			modify: func(c *Config) {
				c.MaxConcurrentStaleReplacements = 1
			},
		},
		{
			name: "max concurrent stale replacements is large",
			modify: func(c *Config) {
				c.MaxConcurrentStaleReplacements = 100
			},
		},
		{
			name: "token ttl is 0",
			modify: func(c *Config) {
				c.TokenTTL = 0
			},
			wantErr: "token ttl must be positive",
		},
		{
			name: "token ttl is negative",
			modify: func(c *Config) {
				c.TokenTTL = -time.Hour
			},
			wantErr: "token ttl must be positive",
		},
		{
			name: "token expiry margin is 0",
			modify: func(c *Config) {
				c.TokenExpiryMargin = 0
			},
			wantErr: "token expiry margin must be positive",
		},
		{
			name: "token expiry margin equals token ttl",
			modify: func(c *Config) {
				c.TokenTTL = time.Hour
				c.TokenExpiryMargin = time.Hour
			},
			wantErr: "token expiry margin must be less than token ttl",
		},
		{
			name: "token expiry margin exceeds token ttl",
			modify: func(c *Config) {
				c.TokenTTL = time.Hour
				c.TokenExpiryMargin = 2 * time.Hour
			},
			wantErr: "token expiry margin must be less than token ttl",
		},
		{
			name: "token expiry margin within downscale stabilization window",
			modify: func(c *Config) {
				c.HPADownscaleStabilization = 10 * time.Minute
				c.TokenExpiryMargin = 10 * time.Minute
			},
			wantErr: "token expiry margin must be greater than",
		},
		{
			name: "token expiry margin within heartbeat timeout",
			modify: func(c *Config) {
				c.HeartbeatTimeout = 10 * time.Minute
				c.TokenExpiryMargin = 10*time.Minute + c.ScaleInterval
			},
			wantErr: "token expiry margin must be greater than",
		},
		{
			name: "token expiry margin just above the floor",
			modify: func(c *Config) {
				c.TokenExpiryMargin = c.tokenExpiryMarginFloor() + time.Second
			},
		},
		{
			name: "short token ttl with proportional margin",
			modify: func(c *Config) {
				c.TokenTTL = 10 * time.Minute
				c.TokenExpiryMargin = 5 * time.Minute
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := config.New[Config]()
			tc.modify(cfg)

			err := cfg.Validate()

			if tc.wantErr == "" {
				assert.NilError(t, err)
			} else {
				assert.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}
