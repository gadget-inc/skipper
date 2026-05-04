package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gadget-inc/skipper/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Spec is the declarative description of a command. Build turns it into a
// [cobra.Command]. Required: Use, plus either RunE or non-empty Sub.
type Spec struct {
	Use     string
	Short   string
	Long    string
	Args    cobra.PositionalArgs
	Flags   func(fs *pflag.FlagSet)
	Configs []any
	Base    bool
	Sub     []*cobra.Command
	RunE    func(cmd *cobra.Command, args []string) error
}

// Build constructs a cobra command from a Spec, registering flags, wiring up
// BaseConfig when Base is set, and wrapping RunE with Init/Validate dispatch.
func Build(spec Spec) *cobra.Command {
	if spec.Use == "" {
		panic("cmd.Build: Spec.Use is required")
	}
	if spec.RunE == nil && len(spec.Sub) == 0 {
		panic("cmd.Build: Spec must have RunE or Sub")
	}

	cmd := &cobra.Command{
		Use:           spec.Use,
		Short:         spec.Short,
		Long:          spec.Long,
		Args:          spec.Args,
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	var baseCfg *BaseConfig
	if spec.Base {
		baseCfg = NewBaseConfig()
	}

	for _, cfg := range spec.Configs {
		config.Bind(cmd, cfg)
	}
	if spec.Flags != nil {
		spec.Flags(cmd.Flags())
	}
	if spec.Base {
		baseCfg.Bind(cmd)
	}
	for _, sub := range spec.Sub {
		cmd.AddCommand(sub)
	}

	if spec.RunE != nil {
		cmd.RunE = func(c *cobra.Command, args []string) error {
			if spec.Base {
				cleanup, err := baseCfg.Init(c)
				if err != nil {
					return err
				}
				defer cleanup()
			}
			for _, cfg := range spec.Configs {
				if v, ok := cfg.(interface{ Validate() error }); ok {
					if err := v.Validate(); err != nil {
						return fmt.Errorf("invalid configuration: %w", err)
					}
				}
			}
			return spec.RunE(c, args)
		}
	}

	return cmd
}

// Main is the body of every cmd/<name>/main.go. It installs a SIGINT/SIGTERM
// signal context, runs the command, prints any error to stderr, and exits 1
// on failure.
func Main(cmd *cobra.Command) {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := cmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
