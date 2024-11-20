package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/gadget-inc/fusion/internal/log"
	"github.com/spf13/cobra"
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
			if strings.EqualFold(logLevelStr, "trace") {
				logLevel = log.LevelTrace
			} else {
				err := logLevel.UnmarshalText([]byte(logLevelStr))
				if err != nil {
					return fmt.Errorf("failed to parse log level: %w", err)
				}
			}

			logOptions := slog.HandlerOptions{
				Level: logLevel,
				ReplaceAttr: func(groups []string, field slog.Attr) slog.Attr {
					if field.Key == slog.LevelKey {
						if field.Value.Any().(slog.Level) == log.LevelTrace {
							field.Value = slog.StringValue("TRACE")
						}
					}
					return field
				},
			}

			var handler slog.Handler
			if logFormatStr == "json" {
				handler = slog.NewJSONHandler(os.Stderr, &logOptions)
			} else {
				handler = slog.NewTextHandler(os.Stderr, &logOptions)
			}

			log.Init(handler)

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
