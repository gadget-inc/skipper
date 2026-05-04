package main

import (
	"context"
	"strings"

	"github.com/gadget-inc/skipper/internal/dev/doclint"
)

// invoke runs another `dev` subcommand in-process. path is the
// space-separated subcommand path as it appears on the CLI
// ("kube-lint", "lint-docs", "build", "fixture request"); args are
// the arguments and flags that follow it. The invoked subcommand
// sees the supplied context and parses flags exactly as it would at
// top level. Each call rebuilds the cobra root via newRoot() so
// per-call flag state cannot leak between invocations.
func invoke(ctx context.Context, path string, args ...string) error {
	root := newRoot()
	root.SetArgs(append(strings.Fields(path), args...))
	return root.ExecuteContext(ctx)
}

// subcommandNames returns the immediate-child and grandchild subcommand
// names of the `dev` cobra root, in the shape doclint's
// bare-dev-command rule consumes (depth-1 set, depth-2 set). Cobra
// built-ins ("help", "completion") are excluded so they do not get
// flagged as bare-invocation candidates in markdown. The synthetic
// "dev" entry in subSub keeps the rule's existing pattern for
// `direnv exec . dev <subcommand>` lines (the prefix scanner strips
// `direnv exec .`, leaving "dev" as the second token).
func subcommandNames() (sub, subSub map[string]bool) {
	sub = map[string]bool{}
	subSub = map[string]bool{"dev": true}
	for _, c := range newRoot().Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		sub[c.Name()] = true
		for _, g := range c.Commands() {
			if g.Hidden || g.Name() == "help" || g.Name() == "completion" {
				continue
			}
			subSub[g.Name()] = true
		}
	}
	return
}

func init() {
	doclint.SetBareDevSubcommands(subcommandNames())
}
