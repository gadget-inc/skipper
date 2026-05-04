package doclint

import (
	"context"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestForEachProseLine_SkipsAllFencedBlocks(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"intro line",
		"```bash",
		"fenced shell",
		"```",
		"middle line",
		"```",
		"fenced unlabeled",
		"```",
		"```go",
		"fenced go",
		"```",
		"trailing line",
	}, "\n")

	var got []string
	forEachProseLine([]byte(src), func(lineNum int, line string) {
		got = append(got, line)
	})

	assert.DeepEqual(t, got, []string{
		"intro line",
		"middle line",
		"trailing line",
	})
}

func TestForEachProseLine_ReportsCorrectLineNumbers(t *testing.T) {
	t.Parallel()

	src := "first\nsecond\nthird\n"
	type seen struct {
		Num  int
		Text string
	}
	var got []seen
	forEachProseLine([]byte(src), func(n int, line string) {
		got = append(got, seen{n, line})
	})

	assert.DeepEqual(t, got, []seen{
		{1, "first"},
		{2, "second"},
		{3, "third"},
	})
}

func TestForEachShellCodeLine_OnlyShellTaggedFences(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"prose line",
		"```bash",
		"echo bash-line",
		"```",
		"```sh",
		"echo sh-line",
		"```",
		"```shell",
		"echo shell-line",
		"```",
		"```Bash",
		"echo Bash-mixed",
		"```",
		"```go",
		"fmt.Println(\"go\")",
		"```",
	}, "\n")

	var got []string
	forEachShellCodeLine([]byte(src), func(lineNum int, line string) {
		got = append(got, line)
	})

	// Case-sensitive: only ```bash, ```sh, ```shell open shell blocks.
	// Plus the closing ``` line of each is not delivered to the callback.
	assert.DeepEqual(t, got, []string{
		"echo bash-line",
		"echo sh-line",
		"echo shell-line",
	})
}

func TestForEachShellCodeLine_ReportsAbsoluteLineNumbers(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"line 1 prose",    // 1
		"```bash",         // 2
		"line 3 in shell", // 3
		"line 4 in shell", // 4
		"```",             // 5
		"line 6 prose",    // 6
		"```bash",         // 7
		"line 8 in shell", // 8
		"```",             // 9
	}, "\n")

	type seen struct {
		Num  int
		Text string
	}
	var got []seen
	forEachShellCodeLine([]byte(src), func(n int, line string) {
		got = append(got, seen{n, line})
	})

	assert.DeepEqual(t, got, []seen{
		{3, "line 3 in shell"},
		{4, "line 4 in shell"},
		{8, "line 8 in shell"},
	})
}

func TestGitCheckIgnore_KnownIgnoredAndTrackedPaths(t *testing.T) {
	t.Parallel()

	// `tmp/` is gitignored at repo root; `go.mod` is tracked.
	got, err := gitCheckIgnore(context.Background(), []string{"tmp/whatever", "go.mod"})
	assert.NilError(t, err)

	assert.Equal(t, got["tmp/whatever"], true)
	assert.Equal(t, got["go.mod"], false)
}

func TestGitCheckIgnore_EmptyInput(t *testing.T) {
	t.Parallel()

	got, err := gitCheckIgnore(context.Background(), nil)
	assert.NilError(t, err)
	assert.Equal(t, len(got), 0)
}
