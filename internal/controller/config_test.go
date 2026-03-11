package controller

import (
	"testing"

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
