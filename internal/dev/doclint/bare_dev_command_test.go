package doclint

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// fixtureSubcommands and fixtureSubSubcommands mirror the live cobra
// tree's subcommand vocabulary. The literals here drift if the cobra
// tree changes; cmd/dev/invoke_test.go's depth-1 / depth-2 assertions
// catch drift between this fixture and the real names.
var (
	fixtureSubcommands = map[string]bool{
		"up":        true,
		"deploy":    true,
		"test":      true,
		"profile":   true,
		"logs":      true,
		"build":     true,
		"clean":     true,
		"fixture":   true,
		"kube-lint": true,
		"fmt":       true,
		"lint":      true,
		"lint-docs": true,
		"generate":  true,
		"docs":      true,
	}
	fixtureSubSubcommands = map[string]bool{
		"request":   true,
		"websocket": true,
		"load":      true,
		"fetch":     true,
		"merge":     true,
		"analyze":   true,
		"open":      true,
		"build":     true,
		"dev":       true,
	}
)

// withFixtureSubcommands installs the fixture maps for the duration of
// a test and resets them on Cleanup. Tests can no longer run in
// parallel because the maps are now mutable globals; the assertions
// are microsecond-fast so the lost parallelism is below noise.
func withFixtureSubcommands(t *testing.T) {
	t.Helper()
	SetBareDevSubcommands(fixtureSubcommands, fixtureSubSubcommands)
	t.Cleanup(func() {
		SetBareDevSubcommands(map[string]bool{}, map[string]bool{})
	})
}

func TestBareDevCommand_FlagsBareDeployInsideBashBlock(t *testing.T) {
	withFixtureSubcommands(t)

	src := strings.Join([]string{
		"```bash",
		"deploy --only=fixtures",
		"```",
	}, "\n")

	got := scanBareDevCommand(File{Path: "CONTRIBUTING.md", Content: []byte(src)})
	assert.Equal(t, len(got), 1)
	assert.Equal(t, got[0].Token, "deploy")
	assert.Equal(t, got[0].Line, 2)
}

func TestBareDevCommand_DoesNotFlagDevDeploy(t *testing.T) {
	withFixtureSubcommands(t)

	src := strings.Join([]string{
		"```bash",
		"dev deploy --only=fixtures",
		"```",
	}, "\n")

	got := scanBareDevCommand(File{Path: "CONTRIBUTING.md", Content: []byte(src)})
	assert.Equal(t, len(got), 0)
}

func TestBareDevCommand_FlagsDirenvExecBareSubcommand(t *testing.T) {
	withFixtureSubcommands(t)

	src := strings.Join([]string{
		"```bash",
		"direnv exec . deploy --only=fixtures",
		"direnv exec . dev deploy --only=fixtures",
		"```",
	}, "\n")

	got := scanBareDevCommand(File{Path: "CONTRIBUTING.md", Content: []byte(src)})
	assert.Equal(t, len(got), 1)
	assert.Equal(t, got[0].Token, "deploy")
	assert.Equal(t, got[0].Line, 2)
}

func TestBareDevCommand_SkipsCommentsAndContinuations(t *testing.T) {
	withFixtureSubcommands(t)

	src := strings.Join([]string{
		"```bash",
		"# deploy comment is fine",
		"  deploy --indented (continuation skipped)",
		"```",
	}, "\n")

	got := scanBareDevCommand(File{Path: "CONTRIBUTING.md", Content: []byte(src)})
	assert.Equal(t, len(got), 0)
}

func TestBareDevCommand_DoesNotScanProseOrNonShellFences(t *testing.T) {
	withFixtureSubcommands(t)

	src := strings.Join([]string{
		"## Heading",
		"`test` is run via `dev test ./...`",
		"```go",
		"// deploy() does not match outside shell blocks",
		"deploy()",
		"```",
	}, "\n")

	got := scanBareDevCommand(File{Path: "CONTRIBUTING.md", Content: []byte(src)})
	assert.Equal(t, len(got), 0)
}

func TestBareDevCommand_RequiresInvocationShape(t *testing.T) {
	withFixtureSubcommands(t)

	// `test` followed by a sentence word (not a flag/subcommand/path)
	// is plain English -- not a real invocation; do NOT flag.
	src := strings.Join([]string{
		"```bash",
		"test is run via the dev test command",
		"```",
	}, "\n")

	got := scanBareDevCommand(File{Path: "CONTRIBUTING.md", Content: []byte(src)})
	assert.Equal(t, len(got), 0, "plain English continuation must not be flagged")
}

func TestBareDevCommand_FlagsBareTestSubcommand(t *testing.T) {
	withFixtureSubcommands(t)

	src := strings.Join([]string{
		"```bash",
		"test -short ./...",
		"```",
	}, "\n")

	got := scanBareDevCommand(File{Path: "CONTRIBUTING.md", Content: []byte(src)})
	assert.Equal(t, len(got), 1)
	assert.Equal(t, got[0].Token, "test")
}
