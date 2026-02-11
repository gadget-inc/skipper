package cmd

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"gotest.tools/v3/golden"
)

func TestHelpTexts(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		newCmd     func() *cobra.Command
		goldenFile string
	}{
		{
			name:       "controller",
			newCmd:     func() *cobra.Command { return NewController(nil) },
			goldenFile: "help_controller.golden",
		},
		{
			name:       "router",
			newCmd:     func() *cobra.Command { return NewRouter(nil) },
			goldenFile: "help_router.golden",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			cmd := tc.newCmd()
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			cmd.SetArgs([]string{"--help"})

			err := cmd.Execute()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			golden.Assert(t, buf.String(), tc.goldenFile)
		})
	}
}
