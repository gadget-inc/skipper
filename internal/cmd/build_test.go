package cmd

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gotest.tools/v3/assert"
)

// fakeConfig is a minimal config struct for exercising config-binding paths.
type fakeConfig struct {
	Name string `flag:"build-test-name" default:"hello" description:"name"`
}

// fakeValidatableConfig records Validate calls and optionally returns an error.
type fakeValidatableConfig struct {
	Field    string `flag:"build-test-field" description:"field"`
	called   int
	errToRet error
}

func (c *fakeValidatableConfig) Validate() error {
	c.called++
	return c.errToRet
}

// noopRunE is a placeholder RunE that does nothing.
func noopRunE(*cobra.Command, []string) error { return nil }

func TestBuildPanics(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		spec        Spec
		wantMessage string
	}{
		{
			name:        "empty Use",
			spec:        Spec{RunE: func(*cobra.Command, []string) error { return nil }},
			wantMessage: "Use",
		},
		{
			name:        "no RunE and no Sub",
			spec:        Spec{Use: "foo"},
			wantMessage: "RunE or Sub",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				r := recover()
				assert.Assert(t, r != nil, "expected panic, got nil")
				msg, ok := r.(string)
				assert.Assert(t, ok, "expected string panic, got %T", r)
				assert.Assert(t, strings.Contains(msg, tc.wantMessage), "panic %q does not mention %q", msg, tc.wantMessage)
			}()

			Build(tc.spec)
		})
	}
}

func TestBuildRegistersConfigsFlags(t *testing.T) {
	t.Parallel()

	cfg := &fakeConfig{}
	cmd := Build(Spec{
		Use:     "foo",
		Configs: []any{cfg},
		RunE:    noopRunE,
	})

	flag := cmd.Flags().Lookup("build-test-name")
	assert.Assert(t, flag != nil, "expected Configs flag to be registered")
}

func TestBuildInvokesFlagsCallback(t *testing.T) {
	t.Parallel()

	var captured *pflag.FlagSet
	var x string
	cmd := Build(Spec{
		Use: "foo",
		Flags: func(fs *pflag.FlagSet) {
			captured = fs
			fs.StringVar(&x, "flag-x", "", "")
		},
		RunE: noopRunE,
	})

	assert.Assert(t, captured != nil, "Flags callback was not invoked")
	assert.Equal(t, captured, cmd.Flags(), "Flags callback received wrong flag set")
	assert.Assert(t, cmd.Flags().Lookup("flag-x") != nil, "hand-rolled flag missing from command")
}

func TestBuildSilencesErrorsAndUsage(t *testing.T) {
	t.Parallel()

	cmd := Build(Spec{Use: "foo", RunE: noopRunE})
	assert.Equal(t, cmd.SilenceErrors, true)
	assert.Equal(t, cmd.SilenceUsage, true)
}

func TestBuildBaseRegistersPersistentFlags(t *testing.T) {
	t.Parallel()

	cmd := Build(Spec{Use: "foo", Base: true, RunE: noopRunE})

	for _, name := range []string{"log-level", "pprof", "telemetry"} {
		assert.Assert(t, cmd.PersistentFlags().Lookup(name) != nil, "expected persistent flag %q", name)
	}
}

func TestBuildValidateDispatch(t *testing.T) {
	t.Parallel()

	t.Run("propagates validation error wrapped as invalid configuration", func(t *testing.T) {
		t.Parallel()

		cfg := &fakeValidatableConfig{errToRet: errors.New("bad value")}
		var ranUserRunE bool
		cmd := Build(Spec{
			Use:     "foo",
			Configs: []any{cfg},
			RunE: func(*cobra.Command, []string) error {
				ranUserRunE = true
				return nil
			},
		})
		cmd.SetArgs(nil)

		err := cmd.Execute()
		assert.ErrorContains(t, err, "invalid configuration: bad value")
		assert.Equal(t, ranUserRunE, false, "user RunE must not run when Validate fails")
		assert.Equal(t, cfg.called, 1, "Validate should be called exactly once")
	})

	t.Run("invokes RunE when Validate returns nil", func(t *testing.T) {
		t.Parallel()

		cfg := &fakeValidatableConfig{}
		var ranUserRunE bool
		cmd := Build(Spec{
			Use:     "foo",
			Configs: []any{cfg},
			RunE: func(*cobra.Command, []string) error {
				ranUserRunE = true
				return nil
			},
		})
		cmd.SetArgs(nil)

		err := cmd.Execute()
		assert.NilError(t, err)
		assert.Equal(t, ranUserRunE, true)
		assert.Equal(t, cfg.called, 1)
	})
}

func TestBuildAddsSubcommands(t *testing.T) {
	t.Parallel()

	child := Build(Spec{Use: "child", RunE: noopRunE})
	parent := Build(Spec{
		Use: "parent",
		Sub: []*cobra.Command{child},
	})

	assert.Equal(t, len(parent.Commands()), 1)
	assert.Equal(t, parent.Commands()[0], child)
}

func TestBuildBaseSuccessInvokesRunE(t *testing.T) {
	t.Parallel()

	var ranUserRunE bool
	cmd := Build(Spec{
		Use:  "foo",
		Base: true,
		RunE: func(*cobra.Command, []string) error {
			ranUserRunE = true
			return nil
		},
	})
	// Disable pprof and telemetry to avoid binding network ports in tests.
	cmd.SetArgs([]string{"--pprof=false", "--telemetry=false"})

	err := cmd.Execute()
	assert.NilError(t, err)
	assert.Equal(t, ranUserRunE, true, "RunE must run on the Init success path")
}

func TestBuildBaseInitFailurePropagates(t *testing.T) {
	t.Parallel()

	var ranUserRunE bool
	cmd := Build(Spec{
		Use:  "foo",
		Base: true,
		RunE: func(*cobra.Command, []string) error {
			ranUserRunE = true
			return nil
		},
	})
	cmd.SetArgs([]string{"--log-format=invalid"})

	err := cmd.Execute()
	assert.ErrorContains(t, err, "invalid log format")
	assert.Equal(t, ranUserRunE, false, "user RunE must not run when Base init fails")
}

func TestMainHelpExitsCleanly(t *testing.T) {
	t.Parallel()

	cmd := Build(Spec{Use: "foo", Short: "test", RunE: noopRunE})
	cmd.SetArgs([]string{"--help"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	// On the success path, Main does not call os.Exit; it returns normally.
	// If Main called os.Exit(1) here, the test process would die.
	Main(cmd)
}

func TestBuildSubOnlyParentPrintsHelpOnBareInvocation(t *testing.T) {
	t.Parallel()

	child := Build(Spec{Use: "child", Short: "child help text", RunE: noopRunE})
	parent := Build(Spec{
		Use:   "parent",
		Short: "parent help text",
		Sub:   []*cobra.Command{child},
	})

	var out bytes.Buffer
	parent.SetOut(&out)
	parent.SetErr(&out)
	parent.SetArgs(nil)

	err := parent.Execute()
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(out.String(), "child"), "expected help to list child command, got: %s", out.String())
	assert.Assert(t, strings.Contains(out.String(), "Usage:"), "expected help to contain Usage section, got: %s", out.String())
}
