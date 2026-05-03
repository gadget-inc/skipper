package main

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/gadget-inc/skipper/internal/cmd"
	"github.com/gadget-inc/skipper/internal/dev/devenv"
	"github.com/gadget-inc/skipper/internal/dev/exec"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const defaultLogsNamespaces = "skipper-development,skipper-development-fixtures,skipper-test,skipper-test-fixtures"

var logsComponentPods = map[string]string{
	"controller": "skipper-controller",
	"router":     "skipper-router",
	"fixtures":   "skipper-fixtures",
	"all":        ".",
}

var logsValidLevels = []string{"trace", "debug", "info", "warn", "error"}

// newLogsCmd wraps stern with: component selectors, level filtering
// with --errors shortcut, time windows, follow / tail control, and
// grep include / exclude patterns.
func newLogsCmd() *cobra.Command {
	var (
		namespace string
		component string
		level     string
		errorsOn  bool
		since     string
		follow    bool
		tail      string
		grep      string
		exclude   string
		output    string
	)

	return cmd.Build(cmd.Spec{
		Use:   "logs [flags]",
		Short: "Stream or view logs from Skipper pods (via stern)",
		Flags: func(fs *pflag.FlagSet) {
			fs.StringVarP(&namespace, "namespace", "n", defaultLogsNamespaces, "Namespaces to stream from")
			fs.StringVarP(&component, "component", "c", "all", "Filter by component: controller, router, fixtures, all")
			fs.StringVarP(&level, "level", "l", "", "Filter by log level: trace, debug, info, warn, error")
			fs.BoolVar(&errorsOn, "errors", false, "Shortcut for --level=error")
			fs.StringVarP(&since, "since", "s", "", "Only show logs newer than the given duration (e.g. 5m, 1h)")
			fs.BoolVarP(&follow, "follow", "f", false, "Stream logs continuously")
			fs.StringVarP(&tail, "tail", "t", "", "Show only the last N lines (default 1000 when not following)")
			fs.StringVarP(&grep, "grep", "g", "", "Filter logs matching the given regex")
			fs.StringVarP(&exclude, "exclude", "E", "", "Exclude logs matching the given regex")
			fs.StringVarP(&output, "output", "o", "text", "Output format: text, json, raw")
		},
		RunE: func(c *cobra.Command, args []string) error {
			if errorsOn {
				level = "error"
			}
			if !follow && tail == "" {
				tail = "1000"
			}

			podPattern, ok := logsComponentPods[component]
			if !ok {
				return fmt.Errorf("unknown component %q (available: controller, router, fixtures, all)", component)
			}

			if output != "text" && output != "json" && output != "raw" {
				return fmt.Errorf("unknown output format %q (available: text, json, raw)", output)
			}

			ctx := os.Getenv("SKIPPER_KUBECTL_CONTEXT")
			if ctx == "" {
				ctx = "orbstack"
			}

			sternArgs := []string{podPattern, "--context=" + ctx, "--namespace=" + namespace}
			switch output {
			case "text":
				sternArgs = append(sternArgs, `--template={{ printf "%s/%s: %s\n" .Namespace .PodName .Message }}`)
			case "json":
				sternArgs = append(sternArgs, "--output=json")
			}
			if since != "" {
				sternArgs = append(sternArgs, "--since="+since)
			}
			if tail != "" {
				sternArgs = append(sternArgs, "--tail="+tail)
			}
			if !follow {
				sternArgs = append(sternArgs, "--no-follow")
			}
			if grep != "" {
				sternArgs = append(sternArgs, "--include="+grep)
			}
			if exclude != "" {
				sternArgs = append(sternArgs, "--exclude="+exclude)
			}
			if level != "" {
				lvl := strings.ToLower(level)
				idx := slices.Index(logsValidLevels, lvl)
				if idx < 0 {
					return fmt.Errorf("unknown level %q (available: %s)", level, strings.Join(logsValidLevels, ", "))
				}
				match := strings.Join(logsValidLevels[idx:], "|")
				// Match level=INFO, "level":"INFO", or [INFO]; (?i) is Go regex's case-insensitive flag.
				pattern := fmt.Sprintf(`(?i)("?level"?[=:]\s*"?(%s)|(\[\s*(%s)\s*\]))`, match, match)
				sternArgs = append(sternArgs, "--include="+pattern)
			}
			if devenv.IsCI() {
				sternArgs = append(sternArgs, "--color=never")
			}

			return exec.Run(c.Context(), "stern", sternArgs)
		},
	})
}
