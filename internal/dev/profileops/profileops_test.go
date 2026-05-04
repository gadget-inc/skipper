package profileops_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/golden"

	"github.com/gadget-inc/skipper/internal/dev/profileops"
)

// timezoneTimeLine strips the timezone-sensitive `Time:` header line
// pprof prints so golden files stay stable across DST transitions.
var timezoneTimeLine = regexp.MustCompile(`(?m)^Time: .*\n`)

// stripTimezoneTime removes the volatile "Time: <local timestamp>"
// line from pprof output so goldens are timezone-independent.
func stripTimezoneTime(s string) string {
	return timezoneTimeLine.ReplaceAllString(s, "")
}

func TestFindProfiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("WORKSPACE_DIR", tmp)
	dir := filepath.Join(tmp, "tmp", "pprof", "controller")
	assert.NilError(t, os.MkdirAll(dir, 0o755))

	cases := []struct {
		name    string
		layout  []string
		pattern string
		regex   *regexp.Regexp
		want    []string
	}{
		{
			name: "filters and sorts by numeric index",
			layout: []string{
				"my-pod-heap-002.pb.gz",
				"my-pod-heap-001.pb.gz",
				"my-pod-heap-010.pb.gz",
				"unrelated-file.txt",
			},
			pattern: "my-pod-heap-*.pb.gz",
			regex:   regexp.MustCompile(`my-pod-heap-(\d+)\.pb\.gz`),
			want: []string{
				"my-pod-heap-001.pb.gz",
				"my-pod-heap-002.pb.gz",
				"my-pod-heap-010.pb.gz",
			},
		},
		{
			name: "sorts numerically not lexicographically",
			layout: []string{
				"pod-cpu-009.pb.gz",
				"pod-cpu-010.pb.gz",
				"pod-cpu-002.pb.gz",
			},
			pattern: "pod-cpu-*.pb.gz",
			regex:   regexp.MustCompile(`pod-cpu-(\d+)\.pb\.gz`),
			want: []string{
				"pod-cpu-002.pb.gz",
				"pod-cpu-009.pb.gz",
				"pod-cpu-010.pb.gz",
			},
		},
		{
			name:    "returns empty when no matches",
			layout:  []string{},
			pattern: "missing-*.pb.gz",
			regex:   regexp.MustCompile(`missing-(\d+)\.pb\.gz`),
			want:    nil,
		},
		{
			name: "regex respects metacharacter escapes",
			layout: []string{
				"pod.name-heap-001.pb.gz",
				"pod.name-heap-002.pb.gz",
				"podXname-heap-003.pb.gz",
			},
			pattern: "pod*name-heap-*.pb.gz",
			regex:   regexp.MustCompile(regexp.QuoteMeta("pod.name") + `-heap-(\d+)\.pb\.gz`),
			want: []string{
				"pod.name-heap-001.pb.gz",
				"pod.name-heap-002.pb.gz",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			subDir := filepath.Join(dir, tc.name)
			assert.NilError(t, os.MkdirAll(subDir, 0o755))
			for _, name := range tc.layout {
				assert.NilError(t, os.WriteFile(filepath.Join(subDir, name), []byte("x"), 0o644))
			}

			got, err := profileops.FindProfiles(filepath.Join("tmp", "pprof", "controller", tc.name), tc.pattern, tc.regex)
			assert.NilError(t, err)

			gotBase := make([]string, len(got))
			for i, p := range got {
				gotBase[i] = filepath.Base(p)
			}
			if len(tc.want) == 0 {
				assert.Equal(t, len(gotBase), 0)
			} else {
				assert.DeepEqual(t, gotBase, tc.want)
			}
		})
	}
}

func TestAnalyzeArgs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("WORKSPACE_DIR", tmp)
	cases := []struct {
		name    string
		opts    profileops.AnalyzeOptions
		wantErr string
		// argsContain checks that the specified substrings are all
		// present in the joined arg list.
		argsContain []string
		// argsAbsent checks that none of these substrings appear.
		argsAbsent []string
		// pathSuffix checks the resolved profile path ends with this.
		pathSuffix string
	}{
		{
			name:    "rejects no profile and no pgo",
			opts:    profileops.AnalyzeOptions{},
			wantErr: "no profile provided (use --pgo or pass a file path)",
		},
		{
			name:    "rejects unknown mode",
			opts:    profileops.AnalyzeOptions{PGO: true, Mode: "invalid"},
			wantErr: "invalid mode: invalid (must be one of top, peek, source, diff)",
		},
		{
			name:    "peek without function",
			opts:    profileops.AnalyzeOptions{PGO: true, Mode: "peek"},
			wantErr: "--function is required for --mode=peek",
		},
		{
			name:    "source without function",
			opts:    profileops.AnalyzeOptions{PGO: true, Mode: "source"},
			wantErr: "--function is required for --mode=source",
		},
		{
			name:    "diff without diff-base",
			opts:    profileops.AnalyzeOptions{PGO: true, Mode: "diff"},
			wantErr: "--diff-base is required for --mode=diff",
		},
		{
			name:        "default top + nodecount=20 with --pgo",
			opts:        profileops.AnalyzeOptions{PGO: true},
			argsContain: []string{"-top", "-nodecount=20"},
			pathSuffix:  filepath.Join("cmd", "controller", "default.pgo"),
		},
		{
			name:        "router PGO selects the router profile",
			opts:        profileops.AnalyzeOptions{PGO: true, Component: "router"},
			argsContain: []string{"-top"},
			pathSuffix:  filepath.Join("cmd", "router", "default.pgo"),
		},
		{
			name:        "positional file path",
			opts:        profileops.AnalyzeOptions{Profile: "tmp/pprof/my.pb.gz"},
			argsContain: []string{"-top"},
			pathSuffix:  filepath.Join("tmp", "pprof", "my.pb.gz"),
		},
		{
			name:        "cumulative top",
			opts:        profileops.AnalyzeOptions{PGO: true, Cumulative: true},
			argsContain: []string{"-cum"},
		},
		{
			name:        "custom nodecount",
			opts:        profileops.AnalyzeOptions{PGO: true, NodeCount: 50},
			argsContain: []string{"-nodecount=50"},
		},
		{
			name:        "peek mode",
			opts:        profileops.AnalyzeOptions{PGO: true, Mode: "peek", Function: "HashRing"},
			argsContain: []string{"-peek=HashRing"},
			argsAbsent:  []string{"-top"},
		},
		{
			name:        "source mode",
			opts:        profileops.AnalyzeOptions{PGO: true, Mode: "source", Function: "Get"},
			argsContain: []string{"-list=Get"},
			argsAbsent:  []string{"-top"},
		},
		{
			name:        "diff mode",
			opts:        profileops.AnalyzeOptions{Mode: "diff", DiffBase: "tmp/before.pb.gz", Profile: "tmp/after.pb.gz"},
			argsContain: []string{"-top", "-diff_base=" + filepath.Join(tmp, "tmp", "before.pb.gz")},
			pathSuffix:  filepath.Join("tmp", "after.pb.gz"),
		},
		{
			name:        "diff mode honors --cum and --nodecount",
			opts:        profileops.AnalyzeOptions{Mode: "diff", DiffBase: "tmp/before.pb.gz", Profile: "tmp/after.pb.gz", Cumulative: true, NodeCount: 10},
			argsContain: []string{"-cum", "-nodecount=10"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args, profilePath, err := profileops.AnalyzeArgs(tc.opts)
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			assert.NilError(t, err)
			joined := strings.Join(args, " ")
			for _, sub := range tc.argsContain {
				assert.Assert(t, strings.Contains(joined, sub), "args %q missing %q", joined, sub)
			}
			for _, sub := range tc.argsAbsent {
				assert.Assert(t, !strings.Contains(joined, sub), "args %q unexpectedly contains %q", joined, sub)
			}
			if tc.pathSuffix != "" {
				assert.Assert(t, strings.HasSuffix(profilePath, tc.pathSuffix),
					"profile %q does not end with %q", profilePath, tc.pathSuffix)
			}
		})
	}
}

func TestAnalyzeAgainstFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping go-tool-pprof integration test in short mode")
	}

	fixture := filepath.Join("testdata", "sample-cpu-001.pb.gz")
	abs, err := filepath.Abs(fixture)
	assert.NilError(t, err)

	// Update goldens with: go test ./internal/dev/profileops/... -update
	cases := []struct {
		name   string
		opts   profileops.AnalyzeOptions
		golden string
	}{
		{
			name:   "top",
			opts:   profileops.AnalyzeOptions{Profile: abs, Mode: "top", NodeCount: 3},
			golden: "analyze-top.golden",
		},
		{
			name:   "top-cum",
			opts:   profileops.AnalyzeOptions{Profile: abs, Mode: "top", NodeCount: 3, Cumulative: true},
			golden: "analyze-top-cum.golden",
		},
		{
			name:   "peek-by-regex",
			opts:   profileops.AnalyzeOptions{Profile: abs, Mode: "peek", Function: "runtime.memmove"},
			golden: "analyze-peek.golden",
		},
		{
			name:   "diff-against-itself",
			opts:   profileops.AnalyzeOptions{Profile: abs, Mode: "diff", DiffBase: abs, NodeCount: 3},
			golden: "analyze-diff.golden",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			tc.opts.Stdout = &buf
			tc.opts.Stderr = io.Discard
			err := profileops.Analyze(context.Background(), tc.opts)
			assert.NilError(t, err)
			golden.Assert(t, stripTimezoneTime(buf.String()), tc.golden)
		})
	}
}

func TestAnalyzeMissingProfile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("WORKSPACE_DIR", tmp)

	err := profileops.Analyze(context.Background(), profileops.AnalyzeOptions{
		Profile: "tmp/missing.pb.gz",
		Mode:    "top",
	})
	assert.ErrorContains(t, err, "profile not found: tmp/missing.pb.gz")

	err = profileops.Analyze(context.Background(), profileops.AnalyzeOptions{
		PGO: true,
	})
	assert.ErrorContains(t, err, "profile not found: cmd/controller/default.pgo")
}

func TestMergeAgainstFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping go-tool-pprof integration test in short mode")
	}

	tmp := t.TempDir()
	t.Setenv("WORKSPACE_DIR", tmp)

	repoTestData, err := filepath.Abs("testdata")
	assert.NilError(t, err)
	productionDir := filepath.Join(tmp, "tmp", "pprof", "production", "controller")
	assert.NilError(t, os.MkdirAll(productionDir, 0o755))
	for _, name := range []string{"sample-cpu-001.pb.gz", "sample-cpu-002.pb.gz"} {
		src, err := os.ReadFile(filepath.Join(repoTestData, name))
		assert.NilError(t, err)
		assert.NilError(t, os.WriteFile(filepath.Join(productionDir, name), src, 0o644))
	}
	assert.NilError(t, os.MkdirAll(filepath.Join(tmp, "cmd", "controller"), 0o755))

	t.Run("dry-run does not write the pgo file", func(t *testing.T) {
		var buf bytes.Buffer
		err := profileops.Merge(context.Background(), profileops.MergeOptions{
			Component: "controller",
			DryRun:    true,
			Stdout:    &buf,
			Stderr:    io.Discard,
		})
		assert.NilError(t, err)
		assert.Assert(t, strings.Contains(buf.String(), "merging 2 CPU profile(s)"))
		_, statErr := os.Stat(filepath.Join(tmp, "cmd", "controller", "default.pgo"))
		assert.Assert(t, os.IsNotExist(statErr), "default.pgo should not exist after dry-run")
	})

	t.Run("merge writes pgo file", func(t *testing.T) {
		var buf bytes.Buffer
		err := profileops.Merge(context.Background(), profileops.MergeOptions{
			Component: "controller",
			Stdout:    &buf,
			Stderr:    io.Discard,
		})
		assert.NilError(t, err)
		info, statErr := os.Stat(filepath.Join(tmp, "cmd", "controller", "default.pgo"))
		assert.NilError(t, statErr)
		assert.Assert(t, info.Size() > 0)
	})

	t.Run("clean removes source profiles", func(t *testing.T) {
		// Re-stage the fixtures because the previous subtest mutated state.
		for _, name := range []string{"sample-cpu-001.pb.gz", "sample-cpu-002.pb.gz"} {
			src, err := os.ReadFile(filepath.Join(repoTestData, name))
			assert.NilError(t, err)
			assert.NilError(t, os.WriteFile(filepath.Join(productionDir, name), src, 0o644))
		}
		var buf bytes.Buffer
		err := profileops.Merge(context.Background(), profileops.MergeOptions{
			Component: "controller",
			Clean:     true,
			Stdout:    &buf,
			Stderr:    io.Discard,
		})
		assert.NilError(t, err)
		_, statErr := os.Stat(filepath.Join(productionDir, "sample-cpu-001.pb.gz"))
		assert.Assert(t, os.IsNotExist(statErr))
		_, statErr = os.Stat(filepath.Join(productionDir, "sample-cpu-002.pb.gz"))
		assert.Assert(t, os.IsNotExist(statErr))
	})

	t.Run("no profiles is not an error", func(t *testing.T) {
		emptyTmp := t.TempDir()
		t.Setenv("WORKSPACE_DIR", emptyTmp)
		var buf bytes.Buffer
		err := profileops.Merge(context.Background(), profileops.MergeOptions{
			Stdout: &buf,
			Stderr: io.Discard,
		})
		assert.NilError(t, err)
		assert.Assert(t, strings.Contains(buf.String(), "No CPU profiles found"))
	})
}

func TestMergeUnknownComponent(t *testing.T) {
	t.Parallel()

	err := profileops.Merge(context.Background(), profileops.MergeOptions{
		Component: "bogus",
		Stdout:    io.Discard,
		Stderr:    io.Discard,
	})
	assert.ErrorContains(t, err, `unknown component "bogus"`)
}

func TestMergeProfilesSinglePassesThrough(t *testing.T) {
	t.Parallel()

	got, err := profileops.MergeProfiles(context.Background(), []string{"/tmp/single.pb.gz"})
	assert.NilError(t, err)
	assert.Equal(t, got, "/tmp/single.pb.gz")
}

func TestMergeProfilesMultipleAgainstFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping go-tool-pprof integration test in short mode")
	}

	tmp := t.TempDir()
	t.Setenv("WORKSPACE_DIR", tmp)

	repoTestData, err := filepath.Abs("testdata")
	assert.NilError(t, err)
	a := filepath.Join(repoTestData, "sample-cpu-001.pb.gz")
	b := filepath.Join(repoTestData, "sample-cpu-002.pb.gz")

	merged, err := profileops.MergeProfiles(context.Background(), []string{a, b})
	assert.NilError(t, err)
	t.Cleanup(func() { _ = os.Remove(merged) })

	info, err := os.Stat(merged)
	assert.NilError(t, err)
	assert.Assert(t, info.Size() > 0)
	assert.Assert(t, strings.HasPrefix(filepath.Base(merged), "merged-diff-base-"))
}
