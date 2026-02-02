package cmd

import (
	"bytes"
	"testing"

	"gotest.tools/v3/golden"
)

func TestHelpTexts(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		args       []string
		goldenFile string
	}{
		{
			name:       "root",
			args:       []string{"--help"},
			goldenFile: "help_root.golden",
		},
		{
			name:       "controller",
			args:       []string{"controller", "--help"},
			goldenFile: "help_controller.golden",
		},
		{
			name:       "router",
			args:       []string{"router", "--help"},
			goldenFile: "help_router.golden",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			cmd := NewRoot()
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			cmd.SetArgs(tc.args)

			err := cmd.Execute()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			golden.Assert(t, buf.String(), tc.goldenFile)
		})
	}
}
