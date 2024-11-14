package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

func NewCmdRoot() *cobra.Command {
	var (
		logLevelStr  string
		logFormatStr string
	)

	cmd := &cobra.Command{
		Use: "fusion",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			var logLevel slog.Level
			err := logLevel.UnmarshalText([]byte(logLevelStr))
			if err != nil {
				return fmt.Errorf("failed to parse log level: %w", err)
			}

			if logFormatStr == "json" {
				slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))
			} else {
				slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))
			}

			klog.SetSlogLogger(slog.Default())

			return nil
		},
	}

	cmd.AddCommand(NewCmdController())
	cmd.AddCommand(NewCmdRouter())

	cmd.PersistentFlags().StringVar(&logLevelStr, "log-level", "info", "log level")
	cmd.PersistentFlags().StringVar(&logFormatStr, "log-format", "json", "log format")
	return cmd
}

func Execute(ctx context.Context) error {
	return NewCmdRoot().ExecuteContext(ctx)
}
