// Package profileops wraps the `go tool pprof` and `kubectl exec`
// shellouts that the dev-tooling profile commands need: fetching a
// profile from a running pod, opening a saved profile in the pprof UI,
// merging CPU profiles into a default.pgo file for profile-guided
// optimization, and analyzing profiles in text mode (top / peek /
// source / diff).
//
// The package owns no global state; each public function takes an
// options struct and an io.Writer for human-facing output. Argument
// construction is split into small pure helpers so tests can verify
// the exact shellout invocation without spawning a process.
package profileops

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gadget-inc/skipper/internal/dev/exec"
)

// stalenessThreshold is the gap between the oldest and newest profile
// in a merge set above which Merge prints a "consider --clean" warning.
const stalenessThreshold = 7 * 24 * time.Hour

// Workspace returns the absolute path of the repository root.
func Workspace() string {
	if d := os.Getenv("WORKSPACE_DIR"); d != "" {
		return d
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// FindProfiles globs <dir>/<pattern> (relative to the workspace root),
// keeps entries whose basename matches re, and sorts ascending by the
// numeric capture group at re's first submatch.
func FindProfiles(dir, pattern string, re *regexp.Regexp) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(Workspace(), dir, pattern))
	if err != nil {
		return nil, fmt.Errorf("glob %s/%s: %w", dir, pattern, err)
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if re.MatchString(filepath.Base(m)) {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return profileIndex(re, out[i]) < profileIndex(re, out[j])
	})
	return out, nil
}

func profileIndex(re *regexp.Regexp, path string) int {
	m := re.FindStringSubmatch(filepath.Base(path))
	if len(m) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// MergeProfiles merges multiple pprof profiles into a single temporary
// file via `go tool pprof -proto`. A single-element slice is returned
// directly; the caller does not need to clean up. For multi-profile
// merges, the returned path lives under tmp/pprof/ and the caller is
// responsible for removing it.
func MergeProfiles(ctx context.Context, profiles []string) (string, error) {
	if len(profiles) == 0 {
		return "", errors.New("MergeProfiles: no profiles")
	}
	if len(profiles) == 1 {
		return profiles[0], nil
	}
	suffix, err := randomHex(8)
	if err != nil {
		return "", err
	}
	merged := filepath.Join(Workspace(), "tmp", "pprof", fmt.Sprintf("merged-diff-base-%s.pb.gz", suffix))
	if err := os.MkdirAll(filepath.Dir(merged), 0o755); err != nil {
		return "", err
	}
	out, err := os.Create(merged)
	if err != nil {
		return "", err
	}
	args := append([]string{"tool", "pprof", "-proto"}, profiles...)
	runErr := exec.Run(ctx, "go", args, exec.Stdout(out))
	if cerr := out.Close(); cerr != nil && runErr == nil {
		runErr = cerr
	}
	if runErr != nil {
		_ = os.Remove(merged)
		return "", runErr
	}
	return merged, nil
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// FetchOptions configures a Fetch invocation.
type FetchOptions struct {
	// Type is "heap" or "cpu".
	Type string
	// Component is the Kubernetes component label to select on
	// ("controller" or "router").
	Component string
	// Production targets the production GKE context+namespace.
	Production bool
	// Seconds is the CPU profile duration; ignored for heap profiles.
	Seconds int
	// Pod, when set, identifies a single pod to fetch from. Mutually
	// exclusive with Spread.
	Pod string
	// Spread fetches one profile from every pod matching the selector.
	Spread bool
	// Web opens the result in `go tool pprof -http=:` after fetching
	// (single-pod only).
	Web bool
	// Diff merges previously fetched profiles and passes them as
	// -diff_base to `go tool pprof` (single-pod only).
	Diff bool
	// Stdout captures human-facing log output. Defaults to os.Stdout.
	Stdout io.Writer
	// Stderr captures human-facing error output. Defaults to os.Stderr.
	Stderr io.Writer
}

// Fetch downloads a pprof profile from one or more running pods and
// optionally opens it in `go tool pprof`. Validates option combinations
// before any kubectl call.
func Fetch(ctx context.Context, opts FetchOptions) error {
	stdout := writerOr(opts.Stdout, os.Stdout)
	stderr := writerOr(opts.Stderr, os.Stderr)

	if opts.Spread && opts.Pod != "" {
		return errors.New("cannot use --spread with a positional pod name")
	}
	if opts.Spread && opts.Web {
		return errors.New("cannot use --spread with --web")
	}
	if opts.Spread && opts.Diff {
		return errors.New("cannot use --spread with --diff")
	}

	endpoint, query, err := pprofEndpoint(opts.Type, opts.Seconds)
	if err != nil {
		return err
	}

	kubectx, namespace := kubeContextNamespace(opts.Production)
	selector := fmt.Sprintf(
		"app.kubernetes.io/name=skipper,app.kubernetes.io/component=%s,app.kubernetes.io/instance=%s",
		opts.Component, namespace,
	)

	pods, err := resolvePods(ctx, opts, kubectx, namespace, selector)
	if err != nil {
		return err
	}

	environment := "development"
	if opts.Production {
		environment = "production"
	}
	profileDir := filepath.Join("tmp", "pprof", environment, opts.Component)

	if opts.Spread {
		return fetchSpread(ctx, opts, pods, kubectx, namespace, endpoint, query, profileDir, stdout, stderr)
	}

	filename, baseProfiles, err := fetchOnePod(ctx, opts, pods[0], "", kubectx, namespace, endpoint, query, profileDir, stdout)
	if err != nil {
		return err
	}

	if !opts.Web && !opts.Diff {
		fmt.Fprintf(stdout, "Open with: profile open %s\n", relPath(filename))
		if opts.Type == "cpu" && !opts.Production {
			fmt.Fprintln(stdout)
			fmt.Fprintln(stdout, "Hint: For PGO, collect CPU profiles from production: profile fetch --type=cpu --production")
		}
		fmt.Fprintln(stdout)
		return nil
	}

	args := []string{"tool", "pprof"}
	if opts.Web {
		args = append(args, "-http=:")
	}

	var mergedDiffBase string
	if opts.Diff && len(baseProfiles) > 0 {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Using base profiles:")
		for _, b := range baseProfiles {
			fmt.Fprintf(stdout, "- %s\n", relPath(b))
		}
		mergedDiffBase, err = MergeProfiles(ctx, baseProfiles)
		if err != nil {
			return err
		}
		args = append(args, "-diff_base", mergedDiffBase)
	}

	args = append(args, filename)

	fmt.Fprintln(stdout)
	runErr := exec.Run(ctx, "go", args, exec.Stdout(stdout), exec.Stderr(stderr))
	if mergedDiffBase != "" && (len(baseProfiles) == 0 || mergedDiffBase != baseProfiles[0]) {
		_ = os.Remove(mergedDiffBase)
	}
	return runErr
}

// pprofEndpoint maps a profile type to the pprof HTTP endpoint and
// query string. Returns an error for unknown types or invalid CPU
// durations.
func pprofEndpoint(profileType string, seconds int) (endpoint, query string, err error) {
	switch profileType {
	case "heap":
		return "heap", "gc=1", nil
	case "cpu":
		if seconds <= 0 {
			return "", "", fmt.Errorf("invalid duration: %d (must be a positive integer)", seconds)
		}
		return "profile", fmt.Sprintf("seconds=%d", seconds), nil
	default:
		return "", "", fmt.Errorf("invalid profile type: %s", profileType)
	}
}

func kubeContextNamespace(production bool) (string, string) {
	if production {
		return "gke_gadget-core-production_us-central1_main", "skipper-production"
	}
	kubectx := os.Getenv("SKIPPER_KUBECTL_CONTEXT")
	if kubectx == "" {
		kubectx = "orbstack"
	}
	return kubectx, "skipper-development"
}

func resolvePods(ctx context.Context, opts FetchOptions, kubectx, namespace, selector string) ([]string, error) {
	if opts.Spread {
		jsonpath := `{range .items[*]}{.metadata.name}{"\n"}{end}`
		out, err := exec.RunOut(ctx, "kubectl", []string{
			"--context=" + kubectx,
			"--namespace=" + namespace,
			"get", "pods",
			"--selector=" + selector,
			"--output=jsonpath=" + jsonpath,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list pods: %w", err)
		}
		pods := []string{}
		for p := range strings.SplitSeq(out, "\n") {
			if p = strings.TrimSpace(p); p != "" {
				pods = append(pods, p)
			}
		}
		if len(pods) == 0 {
			return nil, errors.New("no pods found")
		}
		return pods, nil
	}
	if opts.Pod != "" {
		return []string{opts.Pod}, nil
	}
	out, err := exec.RunOut(ctx, "kubectl", []string{
		"--context=" + kubectx,
		"--namespace=" + namespace,
		"get", "pods",
		"--selector=" + selector,
		"--output=jsonpath={.items[0].metadata.name}",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find pod: %w", err)
	}
	if out = strings.TrimSpace(out); out == "" {
		return nil, errors.New("no pod found")
	}
	return []string{out}, nil
}

func profileFilename(pod, profileType string, index int) string {
	return fmt.Sprintf("%s-%s-%03d.pb.gz", pod, profileType, index)
}

// fetchOnePod fetches a single profile from one pod, atomically
// renaming a temp file to the final destination on success.
func fetchOnePod(
	ctx context.Context, opts FetchOptions, pod, progress, kubectx, namespace, endpoint, query, profileDir string, stdout io.Writer,
) (string, []string, error) {
	re := regexp.MustCompile(fmt.Sprintf(`%s-%s-(\d+)\.pb\.gz`, regexp.QuoteMeta(pod), regexp.QuoteMeta(opts.Type)))
	baseProfiles, err := FindProfiles(profileDir, fmt.Sprintf("%s-%s-*.pb.gz", pod, opts.Type), re)
	if err != nil {
		return "", nil, err
	}
	index := 1
	if len(baseProfiles) > 0 {
		index = profileIndex(re, baseProfiles[len(baseProfiles)-1]) + 1
	}

	absDir := filepath.Join(Workspace(), profileDir)
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return "", nil, err
	}
	filename := filepath.Join(absDir, profileFilename(pod, opts.Type, index))

	duration := ""
	if opts.Type == "cpu" {
		duration = fmt.Sprintf(" (%ds)", opts.Seconds)
	}
	fmt.Fprintf(stdout, "%sFetching %s profile for %s%s\n", progress, opts.Type, pod, duration)
	url := fmt.Sprintf("http://localhost:6060/debug/pprof/%s?%s", endpoint, query)
	suffix, err := randomHex(8)
	if err != nil {
		return "", nil, err
	}
	tmpFilename := fmt.Sprintf("%s.%s.tmp", filename, suffix)
	tmpFile, err := os.Create(tmpFilename)
	if err != nil {
		return "", nil, err
	}
	runErr := exec.Run(ctx, "kubectl", []string{
		"--context=" + kubectx,
		"--namespace=" + namespace,
		"exec", pod, "--",
		"curl", "-sf", url,
	}, exec.Stdout(tmpFile))
	if cerr := tmpFile.Close(); cerr != nil && runErr == nil {
		runErr = cerr
	}
	if runErr != nil {
		_ = os.Remove(tmpFilename)
		return "", nil, fmt.Errorf("failed to fetch profile from %s: %w", pod, runErr)
	}
	if err := os.Rename(tmpFilename, filename); err != nil {
		_ = os.Remove(tmpFilename)
		return "", nil, err
	}

	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "Profile saved to %s\n", relPath(filename))

	return filename, baseProfiles, nil
}

func fetchSpread(
	ctx context.Context, opts FetchOptions, pods []string, kubectx, namespace, endpoint, query, profileDir string, stdout, stderr io.Writer,
) error {
	type result struct {
		filename string
		err      error
	}

	results := make([]result, len(pods))
	var wg sync.WaitGroup
	for i, pod := range pods {
		wg.Go(func() {
			progress := fmt.Sprintf("[%d/%d] ", i+1, len(pods))
			f, _, err := fetchOnePod(ctx, opts, pod, progress, kubectx, namespace, endpoint, query, profileDir, stdout)
			results[i] = result{filename: f, err: err}
		})
	}
	wg.Wait()

	successes := 0
	failedPods := []string{}
	for i, r := range results {
		if r.err == nil {
			successes++
		} else {
			failedPods = append(failedPods, pods[i])
			fmt.Fprintf(stderr, "Failed: %s: %s\n", pods[i], shortReason(r.err))
		}
	}

	// Concurrent kubectl exec streams sometimes get connection-reset
	// by the API server under contention; retry sequentially.
	if len(failedPods) > 0 {
		fmt.Fprintln(stdout)
		if successes == 0 {
			fmt.Fprintln(stdout, "All concurrent fetches failed. Retrying sequentially...")
		} else {
			fmt.Fprintf(stdout, "Retrying %d failed pod(s) sequentially...\n", len(failedPods))
		}
		stillFailed := []string{}
		for _, pod := range failedPods {
			_, _, err := fetchOnePod(ctx, opts, pod, "[retry] ", kubectx, namespace, endpoint, query, profileDir, stdout)
			if err == nil {
				successes++
			} else {
				stillFailed = append(stillFailed, pod)
				fmt.Fprintf(stderr, "Retry failed: %s: %s\n", pod, shortReason(err))
			}
		}
		if successes == 0 {
			return fmt.Errorf("all %d fetch(es) failed", len(pods))
		}
		failedPods = stillFailed
	}

	if len(failedPods) > 0 {
		retryFlags := []string{"--type=" + opts.Type}
		if opts.Production {
			retryFlags = append(retryFlags, "--production")
		}
		if opts.Component != "controller" {
			retryFlags = append(retryFlags, "--component="+opts.Component)
		}
		if opts.Type == "cpu" && opts.Seconds != 30 {
			retryFlags = append(retryFlags, fmt.Sprintf("--seconds=%d", opts.Seconds))
		}
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Retry failed pods:")
		for _, pod := range failedPods {
			fmt.Fprintf(stdout, "  profile fetch %s %s\n", strings.Join(retryFlags, " "), pod)
		}
	}

	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "Fetched %d/%d profile(s). Merge with: profile merge\n", successes, len(pods))
	fmt.Fprintln(stdout)
	return nil
}

// shortReason extracts the most useful single-line summary from an
// error. For ProcessError-style wrapped errors, the wrapped message
// itself usually carries the relevant text already.
func shortReason(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	for line := range strings.SplitSeq(msg, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return msg
}

// OpenOptions configures an Open invocation.
type OpenOptions struct {
	Profile string
	Web     bool
	Diff    bool
	Stdout  io.Writer
	Stderr  io.Writer
}

// Open runs `go tool pprof` against an existing profile, optionally
// passing -diff_base built from earlier profiles in the same series.
func Open(ctx context.Context, opts OpenOptions) error {
	stdout := writerOr(opts.Stdout, os.Stdout)
	stderr := writerOr(opts.Stderr, os.Stderr)

	if opts.Profile == "" {
		return errors.New("no profile provided")
	}
	abs := absPath(opts.Profile)
	if _, err := os.Stat(abs); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("profile not found: %s", opts.Profile)
		}
		return err
	}

	profileDir := filepath.Dir(opts.Profile)
	profile := filepath.Base(opts.Profile)
	indexSuffix := regexp.MustCompile(`-\d+\.pb\.gz$`)
	prefix := indexSuffix.ReplaceAllString(profile, "")
	re := regexp.MustCompile(fmt.Sprintf(`%s-(\d+)\.pb\.gz$`, regexp.QuoteMeta(prefix)))
	currentIndex := profileIndex(re, profile)

	var baseProfiles []string
	if opts.Diff {
		all, err := FindProfiles(profileDir, fmt.Sprintf("%s-*.pb.gz", prefix), re)
		if err != nil {
			return err
		}
		for _, p := range all {
			if profileIndex(re, p) < currentIndex {
				baseProfiles = append(baseProfiles, p)
			}
		}
	}

	args := []string{"tool", "pprof"}
	var mergedDiffBase string
	if len(baseProfiles) > 0 {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Using base profiles:")
		for _, b := range baseProfiles {
			fmt.Fprintf(stdout, "- %s\n", relPath(b))
		}
		merged, err := MergeProfiles(ctx, baseProfiles)
		if err != nil {
			return err
		}
		mergedDiffBase = merged
		args = append(args, "-diff_base", mergedDiffBase)
	}
	if opts.Web {
		args = append(args, "-http=:")
	} else {
		args = append(args, "-top")
	}
	args = append(args, abs)

	fmt.Fprintln(stdout)
	runErr := exec.Run(ctx, "go", args, exec.Stdout(stdout), exec.Stderr(stderr))
	if mergedDiffBase != "" && (len(baseProfiles) == 0 || mergedDiffBase != baseProfiles[0]) {
		_ = os.Remove(mergedDiffBase)
	}
	return runErr
}

// MergeOptions configures a Merge invocation.
type MergeOptions struct {
	// Component selects "controller", "router", or "all".
	Component string
	Clean     bool
	DryRun    bool
	Stdout    io.Writer
	Stderr    io.Writer
}

// Merge merges every CPU profile under tmp/pprof/production/<component>
// into cmd/<component>/default.pgo. Prints a staleness or mixed-
// duration warning when applicable; deletes source profiles when
// Clean is set and DryRun is not.
func Merge(ctx context.Context, opts MergeOptions) error {
	stdout := writerOr(opts.Stdout, os.Stdout)

	components := []string{"controller", "router"}
	switch opts.Component {
	case "all", "":
	case "controller", "router":
		components = []string{opts.Component}
	default:
		return fmt.Errorf("unknown component %q (available: controller, router, all)", opts.Component)
	}

	totalProfiles := 0
	for _, component := range components {
		profiles, err := filepath.Glob(filepath.Join(Workspace(), "tmp", "pprof", "production", component, "*-cpu-*.pb.gz"))
		if err != nil {
			return fmt.Errorf("glob %s profiles: %w", component, err)
		}
		sort.Strings(profiles)
		totalProfiles += len(profiles)

		if len(profiles) == 0 {
			fmt.Fprintf(stdout, "No CPU profiles found for %s\n", component)
			continue
		}

		if err := warnStaleness(stdout, component, profiles); err != nil {
			return err
		}
		if err := warnMixedDurations(ctx, stdout, component, profiles); err != nil {
			return err
		}

		output := filepath.Join(Workspace(), "cmd", component, "default.pgo")

		fmt.Fprintf(stdout, "%s: merging %d CPU profile(s)\n", component, len(profiles))
		for _, p := range profiles {
			fmt.Fprintf(stdout, "  %s\n", relPath(p))
		}
		fmt.Fprintf(stdout, "  → %s\n", relPath(output))

		if opts.DryRun {
			continue
		}

		out, err := os.Create(output)
		if err != nil {
			return err
		}
		args := append([]string{"tool", "pprof", "-proto"}, profiles...)
		runErr := exec.Run(ctx, "go", args, exec.Stdout(out))
		if cerr := out.Close(); cerr != nil && runErr == nil {
			runErr = cerr
		}
		if runErr != nil {
			return runErr
		}

		if opts.Clean {
			for _, p := range profiles {
				if err := os.Remove(p); err != nil {
					return err
				}
				fmt.Fprintf(stdout, "  deleted %s\n", relPath(p))
			}
		}

		fmt.Fprintln(stdout)
	}

	if totalProfiles == 0 {
		fmt.Fprintln(stdout, "No CPU profiles found in tmp/pprof/production/")
	}
	return nil
}

func warnStaleness(stdout io.Writer, component string, profiles []string) error {
	var oldest, newest time.Time
	for i, p := range profiles {
		info, err := os.Stat(p)
		if err != nil {
			return err
		}
		mt := info.ModTime()
		if i == 0 || mt.Before(oldest) {
			oldest = mt
		}
		if i == 0 || mt.After(newest) {
			newest = mt
		}
	}
	if span := newest.Sub(oldest); span > stalenessThreshold {
		days := int(span.Round(24*time.Hour) / (24 * time.Hour))
		fmt.Fprintf(stdout, "Warning: %s profiles span %d days — consider using --clean to remove stale profiles\n", component, days)
	}
	return nil
}

var (
	pprofDurationRe = regexp.MustCompile(`Duration:\s*([^,\n]+)`)
	durationPartRe  = regexp.MustCompile(`([\d.]+)(h|m|s)`)
)

func warnMixedDurations(ctx context.Context, stdout io.Writer, component string, profiles []string) error {
	durations := []float64{}
	seen := map[float64]struct{}{}
	for _, p := range profiles {
		raw, err := exec.RunOut(ctx, "go", []string{"tool", "pprof", "-raw", p}, exec.Stderr(io.Discard))
		if err != nil {
			return err
		}
		match := pprofDurationRe.FindStringSubmatch(raw)
		if len(match) < 2 {
			continue
		}
		secs := parsePprofDuration(strings.TrimSpace(match[1]))
		if secs <= 0 {
			continue
		}
		if _, ok := seen[secs]; !ok {
			seen[secs] = struct{}{}
			durations = append(durations, secs)
		}
	}
	if len(durations) > 1 {
		parts := make([]string, len(durations))
		for i, d := range durations {
			parts[i] = fmt.Sprintf("%ss", strconv.FormatFloat(d, 'f', -1, 64))
		}
		fmt.Fprintf(stdout, "Warning: %s profiles have mixed durations (%s) — longer profiles will be overrepresented in the merge\n",
			component, strings.Join(parts, ", "))
	}
	return nil
}

// parsePprofDuration parses pprof's "Duration: 30s" / "1h30m" form
// into seconds. Unrecognized input returns 0.
func parsePprofDuration(s string) float64 {
	matches := durationPartRe.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return 0
	}
	total := 0.0
	for _, m := range matches {
		n, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			continue
		}
		switch m[2] {
		case "h":
			total += n * 3600
		case "m":
			total += n * 60
		default:
			total += n
		}
	}
	return total
}

// AnalyzeOptions configures an Analyze invocation.
type AnalyzeOptions struct {
	// Profile is the path to the profile (mutually exclusive with PGO).
	Profile string
	// PGO resolves the profile to cmd/<Component>/default.pgo.
	PGO bool
	// Component selects which committed PGO profile to analyze when
	// PGO is true.
	Component string
	// Mode is one of "top", "peek", "source", "diff".
	Mode string
	// Function is a regex required for peek and source modes.
	Function string
	// NodeCount limits the result list (top / diff modes).
	NodeCount int
	// Cumulative sorts by cumulative time instead of flat (top / diff).
	Cumulative bool
	// DiffBase is the base profile path for diff mode.
	DiffBase string
	Stdout   io.Writer
	Stderr   io.Writer
}

// AnalyzeArgs builds the `go tool pprof <args> <profile>` argv for the
// requested analysis. Exposed for testing; Analyze is the public entry
// point.
func AnalyzeArgs(opts AnalyzeOptions) ([]string, string, error) {
	validModes := []string{"top", "peek", "source", "diff"}
	if opts.Mode == "" {
		opts.Mode = "top"
	}
	if !slices.Contains(validModes, opts.Mode) {
		return nil, "", fmt.Errorf("invalid mode: %s (must be one of %s)", opts.Mode, strings.Join(validModes, ", "))
	}
	if (opts.Mode == "peek" || opts.Mode == "source") && opts.Function == "" {
		return nil, "", fmt.Errorf("--function is required for --mode=%s", opts.Mode)
	}
	if opts.Mode == "diff" && opts.DiffBase == "" {
		return nil, "", errors.New("--diff-base is required for --mode=diff")
	}

	var filepathArg string
	if opts.PGO {
		component := opts.Component
		if component == "" {
			component = "controller"
		}
		filepathArg = filepath.Join(Workspace(), "cmd", component, "default.pgo")
	} else if opts.Profile != "" {
		filepathArg = absPath(opts.Profile)
	} else {
		return nil, "", errors.New("no profile provided (use --pgo or pass a file path)")
	}

	nodeCount := opts.NodeCount
	if nodeCount <= 0 {
		nodeCount = 20
	}

	args := []string{"tool", "pprof"}
	switch opts.Mode {
	case "top":
		args = append(args, "-top", fmt.Sprintf("-nodecount=%d", nodeCount))
		if opts.Cumulative {
			args = append(args, "-cum")
		}
	case "peek":
		args = append(args, "-peek="+opts.Function)
	case "source":
		args = append(args, "-list="+opts.Function)
	case "diff":
		args = append(args, "-top", fmt.Sprintf("-nodecount=%d", nodeCount))
		if opts.Cumulative {
			args = append(args, "-cum")
		}
		args = append(args, "-diff_base="+absPath(opts.DiffBase))
	}
	args = append(args, filepathArg)
	return args, filepathArg, nil
}

// Analyze runs `go tool pprof` in text mode (top / peek / source /
// diff) and streams stdout to opts.Stdout. The profile path is
// resolved against the workspace root.
func Analyze(ctx context.Context, opts AnalyzeOptions) error {
	stdout := writerOr(opts.Stdout, os.Stdout)
	stderr := writerOr(opts.Stderr, os.Stderr)

	args, profilePath, err := AnalyzeArgs(opts)
	if err != nil {
		return err
	}
	if _, err := os.Stat(profilePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if opts.PGO {
				component := opts.Component
				if component == "" {
					component = "controller"
				}
				return fmt.Errorf("profile not found: cmd/%s/default.pgo", component)
			}
			return fmt.Errorf("profile not found: %s", opts.Profile)
		}
		return err
	}
	return exec.Run(ctx, "go", args, exec.Stdout(stdout), exec.Stderr(stderr))
}

// absPath resolves a path against the workspace root unless the path
// is already absolute.
func absPath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(Workspace(), p)
}

// relPath formats an absolute workspace-rooted path for display.
func relPath(p string) string {
	rel, err := filepath.Rel(Workspace(), p)
	if err != nil {
		return p
	}
	return rel
}

func writerOr(w, fallback io.Writer) io.Writer {
	if w != nil {
		return w
	}
	return fallback
}
