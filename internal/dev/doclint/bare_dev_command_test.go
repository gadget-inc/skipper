package doclint

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestBareDevCommand_FlagsBareDeployInsideBashBlock(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	src := strings.Join([]string{
		"```bash",
		"dev deploy --only=fixtures",
		"```",
	}, "\n")

	got := scanBareDevCommand(File{Path: "CONTRIBUTING.md", Content: []byte(src)})
	assert.Equal(t, len(got), 0)
}

func TestBareDevCommand_FlagsDirenvExecBareSubcommand(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

	src := strings.Join([]string{
		"## Heading",
		"`tests` are run via `dev tests go ./...`",
		"```go",
		"// deploy() does not match outside shell blocks",
		"deploy()",
		"```",
	}, "\n")

	got := scanBareDevCommand(File{Path: "CONTRIBUTING.md", Content: []byte(src)})
	assert.Equal(t, len(got), 0)
}

func TestBareDevCommand_RequiresInvocationShape(t *testing.T) {
	t.Parallel()

	// `tests` followed by a sentence word (not a flag/subcommand/path)
	// is plain English -- not a real invocation; do NOT flag.
	src := strings.Join([]string{
		"```bash",
		"tests are run via the dev tests command",
		"```",
	}, "\n")

	got := scanBareDevCommand(File{Path: "CONTRIBUTING.md", Content: []byte(src)})
	assert.Equal(t, len(got), 0, "plain English continuation must not be flagged")
}

func TestBareDevCommand_FlagsTestsGoInvocation(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"```bash",
		"tests go -short ./...",
		"```",
	}, "\n")

	got := scanBareDevCommand(File{Path: "CONTRIBUTING.md", Content: []byte(src)})
	assert.Equal(t, len(got), 1)
	assert.Equal(t, got[0].Token, "tests")
}
