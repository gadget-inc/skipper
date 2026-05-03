package doclint

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	osexec "os/exec"
	"strings"
)

// forEachProseLine invokes fn for every line in content that is NOT
// inside a fenced code block. A fenced block opens on a line whose
// trimmed prefix is "```" (with any language tag) and closes on the
// next "```" line. Line numbers are 1-based and absolute (they count
// the fence lines too, even though those are skipped).
func forEachProseLine(content []byte, fn func(lineNum int, line string)) {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	inFence := false
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if isFenceLine(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		fn(lineNum, line)
	}
}

// forEachLine invokes fn for every line in content. Line numbers are
// 1-based.
func forEachLine(content []byte, fn func(lineNum int, line string)) {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		fn(lineNum, scanner.Text())
	}
}

// forEachShellCodeLine invokes fn for every line that lies INSIDE a
// fenced code block opened with ```bash, ```sh, or ```shell (the
// language tag is matched case-sensitively and must be exact). Lines
// outside such blocks — including prose and other-language fences —
// are skipped. Line numbers are 1-based and absolute.
func forEachShellCodeLine(content []byte, fn func(lineNum int, line string)) {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	inShell := false
	inOther := false
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimLeft(line, " \t")
		rest, isFence := strings.CutPrefix(trimmed, "```")
		if isFence {
			tag := strings.TrimSpace(rest)
			switch {
			case inShell:
				inShell = false
			case inOther:
				inOther = false
			case tag == "bash" || tag == "sh" || tag == "shell":
				inShell = true
			default:
				inOther = true
			}
			continue
		}
		if inShell {
			fn(lineNum, line)
		}
	}
}

// isFenceLine returns true if line opens or closes a fenced code block.
func isFenceLine(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " \t"), "```")
}

// gitCheckIgnore returns a map from each input path to whether `git
// check-ignore` claims it is ignored. Empty input returns an empty map
// without invoking git. Paths are passed to git as-is (relative to the
// repo root, forward-slash separators).
func gitCheckIgnore(ctx context.Context, paths []string) (map[string]bool, error) {
	out := make(map[string]bool, len(paths))
	if len(paths) == 0 {
		return out, nil
	}

	root, err := repoRoot(ctx)
	if err != nil {
		return nil, err
	}

	cmd := osexec.CommandContext(ctx, "git", "-C", root, "check-ignore", "--stdin", "-z")
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\x00"))
	stdout, err := cmd.Output()
	// `git check-ignore` exits 1 when no paths are ignored, 0 when at
	// least one is, and 128 on a real error. Treat exit 1 as success.
	if err != nil {
		var exitErr *osexec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return nil, fmt.Errorf("git check-ignore: %w", err)
		}
	}

	ignored := make(map[string]struct{})
	for raw := range strings.SplitSeq(string(stdout), "\x00") {
		if raw == "" {
			continue
		}
		ignored[raw] = struct{}{}
	}

	for _, p := range paths {
		_, hit := ignored[p]
		out[p] = hit
	}
	return out, nil
}
