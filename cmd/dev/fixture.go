package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/gadget-inc/skipper/internal/cmd"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Defaults for the local skipper router. SKIPPER_ROUTER_URL overrides
// the URL; the fixture function descriptor is what controller assigns
// to incoming requests through the router.
var (
	defaultRouterURL = envDefault("SKIPPER_ROUTER_URL", "http://127.0.0.1:8081")

	fixtureFunctionDefault = map[string]any{
		"namespace":  "skipper-development-fixtures",
		"deployment": "fixture",
		"tenant":     "tenant-1",
		"metadata":   `{"foo":"bar"}`,
		"scale": map[string]any{
			"min_instances":             0,
			"max_instances":             5,
			"target_cpu_usage_milli":    100,
			"target_memory_usage_mib":   200,
			"target_in_flight_requests": 50,
		},
	}
)

// newFixtureCmd exposes the dev-time fixture workbench: HTTP requests,
// WebSocket sessions, and load tests against the K8s-deployed fixture
// container through the local router.
func newFixtureCmd() *cobra.Command {
	return cmd.Build(cmd.Spec{
		Use:   "fixture",
		Short: "Test the fixture via HTTP, WebSocket, and load",
		Sub: []*cobra.Command{
			newFixtureRequestCmd(),
			newFixtureWebSocketCmd(),
			newFixtureLoadCmd(),
		},
	})
}

// -- request -----------------------------------------------------------

func newFixtureRequestCmd() *cobra.Command {
	var (
		method  string
		path    string
		body    string
		tenant  string
		count   int
		verbose bool
	)

	return cmd.Build(cmd.Spec{
		Use:   "request",
		Short: "Send HTTP request(s) to the fixture",
		Flags: func(fs *pflag.FlagSet) {
			fs.StringVarP(&method, "method", "m", "POST", "HTTP method")
			fs.StringVarP(&path, "path", "p", "/hello", "Request path")
			fs.StringVarP(&body, "body", "b", `{"hello":"world"}`, "Request body JSON")
			fs.StringVarP(&tenant, "tenant", "t", fixtureFunctionDefault["tenant"].(string), "Tenant ID")
			fs.IntVarP(&count, "count", "n", 1, "Number of requests to send")
			fs.BoolVarP(&verbose, "verbose", "v", false, "Show response headers")
		},
		RunE: func(c *cobra.Command, args []string) error {
			if count <= 0 {
				return fmt.Errorf("invalid count: %d (must be a positive integer)", count)
			}
			fn := withTenant(tenant)
			fnHeader, err := json.Marshal(fn)
			if err != nil {
				return err
			}

			client := &http.Client{}
			for i := 0; i < count; i++ {
				if count > 1 {
					fmt.Printf("\033[2m--- request %d/%d ---\033[0m\n", i+1, count)
				}
				if err := sendOne(c.Context(), client, method, path, body, string(fnHeader), verbose); err != nil {
					return err
				}
				if count > 1 && i < count-1 {
					fmt.Println()
				}
			}
			return nil
		},
	})
}

func sendOne(ctx context.Context, client *http.Client, method, path, body, fn string, verbose bool) error {
	url := defaultRouterURL + path
	var bodyReader io.Reader
	if method != "GET" {
		bodyReader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("x-skipper-function", fn)
	req.Header.Set("content-type", "application/json")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	latency := time.Since(start)

	statusColor := "\033[31m"
	if resp.StatusCode < 400 {
		statusColor = "\033[32m"
	}
	fmt.Printf("%s %s  %s%d\033[0m  %s\n", method, path, statusColor, resp.StatusCode, formatLatency(float64(latency.Microseconds())/1000))

	if verbose {
		for k, vs := range resp.Header {
			for _, v := range vs {
				fmt.Printf("\033[2m  %s: %s\033[0m\n", k, v)
			}
		}
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 400 {
		var parsed map[string]any
		if err := json.Unmarshal(bodyBytes, &parsed); err == nil {
			pretty, _ := json.MarshalIndent(parsed, "", "  ")
			fmt.Println(string(pretty))
		} else {
			fmt.Println(string(bodyBytes))
		}
	} else if len(bodyBytes) > 0 {
		fmt.Fprintf(os.Stderr, "\033[31m%s\033[0m\n", string(bodyBytes))
	}
	return nil
}

// -- websocket ---------------------------------------------------------

func newFixtureWebSocketCmd() *cobra.Command {
	var (
		message  string
		interval time.Duration
		count    int
		tenant   string
	)

	wsCmd := cmd.Build(cmd.Spec{
		Use:   "websocket",
		Short: "Open a WebSocket connection to the fixture",
		Flags: func(fs *pflag.FlagSet) {
			fs.StringVar(&message, "message", "ping", "Message to send")
			fs.DurationVarP(&interval, "interval", "i", time.Second, "Interval between messages")
			fs.IntVarP(&count, "count", "n", 0, "Number of messages to send (0 = unlimited)")
			fs.StringVarP(&tenant, "tenant", "t", fixtureFunctionDefault["tenant"].(string), "Tenant ID")
		},
		RunE: func(c *cobra.Command, args []string) error {
			if interval <= 0 {
				return fmt.Errorf("invalid interval: %s (must be > 0)", interval)
			}
			if count < 0 {
				return fmt.Errorf("invalid count: %d (must be >= 0)", count)
			}
			fn := withTenant(tenant)
			fnHeader, err := json.Marshal(fn)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithCancel(c.Context())
			defer cancel()

			conn, _, err := websocket.Dial(ctx, defaultRouterURL, &websocket.DialOptions{
				HTTPHeader: http.Header{"x-skipper-function": []string{string(fnHeader)}},
			})
			if err != nil {
				return err
			}
			defer func() { _ = conn.CloseNow() }()

			fmt.Println("\033[32mconnected\033[0m")
			if err := conn.Write(ctx, websocket.MessageText, []byte(message)); err != nil {
				return err
			}
			sent := 1

			for {
				typ, data, err := conn.Read(ctx)
				if err != nil {
					if errors.Is(err, context.Canceled) {
						break
					}
					var ce websocket.CloseError
					if errors.As(err, &ce) {
						fmt.Printf("\033[2mclosed (%d)\033[0m\n", ce.Code)
						break
					}
					return err
				}
				_ = typ
				fmt.Printf("\033[2mrecv\033[0m  %s\n", data)

				if count > 0 && sent >= count {
					_ = conn.Close(websocket.StatusNormalClosure, "")
					break
				}

				select {
				case <-ctx.Done():
					_ = conn.Close(websocket.StatusNormalClosure, "")
				case <-time.After(interval):
				}
				if ctx.Err() != nil {
					break
				}
				if err := conn.Write(ctx, websocket.MessageText, []byte(message)); err != nil {
					return err
				}
				sent++
				fmt.Printf("\033[2msend\033[0m  %s\n", message)
			}

			fmt.Printf("%d message(s) sent\n", sent)
			return nil
		},
	})
	wsCmd.Aliases = []string{"ws"}
	return wsCmd
}

// -- load --------------------------------------------------------------

const (
	loadReservoirSize    = 10_000
	loadProgressInterval = 250 * time.Millisecond
)

var loadPercentiles = []float64{0.5, 0.9, 0.99, 0.999, 1}

func newFixtureLoadCmd() *cobra.Command {
	var (
		concurrency int
		requests    int
		infinite    bool
		duration    time.Duration
		tenants     int
		noWarmUp    bool
	)

	return cmd.Build(cmd.Spec{
		Use:   "load",
		Short: "Run a load test against the fixture",
		Flags: func(fs *pflag.FlagSet) {
			fs.IntVarP(&concurrency, "concurrency", "c", 10, "Concurrent requests")
			fs.IntVarP(&requests, "requests", "n", 100_000, "Total requests to send")
			fs.BoolVar(&infinite, "infinite", false, "Run until interrupted")
			fs.DurationVarP(&duration, "duration", "d", 0, "Run for a duration instead of request count (e.g. 30s, 1m)")
			fs.IntVarP(&tenants, "tenants", "t", 2, "Number of tenants to rotate through")
			fs.BoolVar(&noWarmUp, "no-warm-up", false, "Skip warm-up (one request per tenant)")
		},
		RunE: func(c *cobra.Command, args []string) error {
			if concurrency <= 0 {
				return fmt.Errorf("invalid concurrency: %d (must be a positive integer)", concurrency)
			}
			if tenants <= 0 {
				return fmt.Errorf("invalid tenants: %d (must be a positive integer)", tenants)
			}
			return runLoad(c.Context(), loadOptions{
				concurrency: concurrency,
				requests:    requests,
				infinite:    infinite,
				duration:    duration,
				tenants:     tenants,
				skipWarmUp:  noWarmUp,
			})
		},
	})
}

type loadOptions struct {
	concurrency int
	requests    int
	infinite    bool
	duration    time.Duration
	tenants     int
	skipWarmUp  bool
}

func runLoad(ctx context.Context, opts loadOptions) error {
	tenants := make([]string, opts.tenants)
	for i := range tenants {
		tenants[i] = fmt.Sprintf("tenant-%d", i+1)
	}

	client := &http.Client{}

	if !opts.skipWarmUp {
		for i, t := range tenants {
			fmt.Fprintf(os.Stdout, "\r\033[K\033[2mWarming up %s (%d/%d)...\033[0m", t, i+1, len(tenants))
			fn := withTenant(t)
			fnHeader, _ := json.Marshal(fn)
			req, err := http.NewRequestWithContext(ctx, "POST", defaultRouterURL, strings.NewReader(`{"hello":"world"}`))
			if err != nil {
				return err
			}
			req.Header.Set("x-skipper-function", string(fnHeader))
			req.Header.Set("content-type", "application/json")
			resp, err := client.Do(req)
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}
		fmt.Fprintf(os.Stdout, "\r\033[K\033[2mWarmed up %d tenant(s)\033[0m\n", len(tenants))
	}

	requestsLabel := fmt.Sprintf("%d requests", opts.requests)
	if opts.infinite {
		requestsLabel = "infinite"
	} else if opts.duration > 0 {
		requestsLabel = fmt.Sprintf("duration %s", opts.duration)
	}
	fmt.Printf("\n\033[1mLoad test\033[0m  %d concurrent, %s, %d tenant(s)\n", opts.concurrency, requestsLabel, opts.tenants)
	fmt.Printf("\033[2mTarget\033[0m     %s\n\n", defaultRouterURL)

	state := &loadState{}
	startedAt := time.Now()
	deadline := time.Time{}
	if opts.duration > 0 {
		deadline = startedAt.Add(opts.duration)
	}

	stopProgress := startProgress(state, &startedAt, opts)
	defer stopProgress()

	gctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if opts.duration > 0 {
		gctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}

	work := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < opts.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range work {
				sendLoadRequest(gctx, client, tenants, state)
			}
		}()
	}

	go func() {
		defer close(work)
		for i := 0; ; i++ {
			if !opts.infinite && opts.duration == 0 && i >= opts.requests {
				return
			}
			select {
			case <-gctx.Done():
				return
			case work <- struct{}{}:
			}
		}
	}()

	wg.Wait()
	stopProgress()
	fmt.Fprintf(os.Stdout, "\r\033[K")

	elapsed := time.Since(startedAt)
	completed := atomic.LoadInt64(&state.completed)
	failures := atomic.LoadInt64(&state.failures)
	succeeded := completed - failures
	rps := float64(completed) / elapsed.Seconds()

	state.mu.Lock()
	sorted := make([]float64, len(state.reservoir))
	copy(sorted, state.reservoir)
	maxLatency := state.maxLatency
	state.mu.Unlock()
	sort.Float64s(sorted)

	fmt.Printf("\033[1mResults\033[0m\n\n")
	errorRate := "0.00"
	if completed > 0 {
		errorRate = fmt.Sprintf("%.2f", float64(failures)/float64(completed)*100)
	}
	failureSuffix := ""
	if failures > 0 {
		failureSuffix = fmt.Sprintf(", \033[31m%d\033[0m failed (%s%%)", failures, errorRate)
	}
	fmt.Printf("  Requests     %d total, \033[32m%d\033[0m succeeded%s\n", completed, succeeded, failureSuffix)
	fmt.Printf("  Duration     %s\n", formatLatency(float64(elapsed.Microseconds())/1000))
	fmt.Printf("  Throughput   \033[1m%d\033[0m req/s\n", int64(math.Round(rps)))

	if len(sorted) > 0 {
		fmt.Printf("\n  Latency\n")
		for _, p := range loadPercentiles {
			label := fmt.Sprintf("p%g", p*100)
			value := getPercentile(sorted, p)
			if p == 1 {
				label = "max"
				value = maxLatency
			}
			fmt.Printf("    %-8s %s\n", label, formatLatency(value))
		}
	}
	fmt.Println()

	if failures > 0 {
		return errors.New("some requests failed")
	}
	return nil
}

type loadState struct {
	mu         sync.Mutex
	reservoir  []float64
	count      int64 // total samples seen
	maxLatency float64
	completed  int64 // atomic
	failures   int64 // atomic
}

func (s *loadState) addLatency(ms float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count++
	if ms > s.maxLatency {
		s.maxLatency = ms
	}
	if len(s.reservoir) < loadReservoirSize {
		s.reservoir = append(s.reservoir, ms)
		return
	}
	// Algorithm R: each new sample has reservoirSize/count probability
	// of replacing one of the existing samples.
	j := rand.IntN(int(s.count))
	if j < loadReservoirSize {
		s.reservoir[j] = ms
	}
}

func sendLoadRequest(ctx context.Context, client *http.Client, tenants []string, state *loadState) {
	tenant := tenants[rand.IntN(len(tenants))]
	fn := withTenant(tenant)
	fnHeader, _ := json.Marshal(fn)

	req, err := http.NewRequestWithContext(ctx, "POST", defaultRouterURL, strings.NewReader(`{"hello":"world"}`))
	if err != nil {
		return
	}
	req.Header.Set("x-skipper-function", string(fnHeader))
	req.Header.Set("content-type", "application/json")

	start := time.Now()
	resp, err := client.Do(req)
	latencyMs := float64(time.Since(start).Microseconds()) / 1000

	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		atomic.AddInt64(&state.completed, 1)
		atomic.AddInt64(&state.failures, 1)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	atomic.AddInt64(&state.completed, 1)
	if resp.StatusCode >= 400 {
		atomic.AddInt64(&state.failures, 1)
	}
	state.addLatency(latencyMs)
}

func startProgress(state *loadState, startedAt *time.Time, opts loadOptions) func() {
	if !isStdoutTTY() {
		return func() {}
	}
	ticker := time.NewTicker(loadProgressInterval)
	stopCh := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopCh:
				ticker.Stop()
				return
			case <-ticker.C:
				elapsed := time.Since(*startedAt)
				completed := atomic.LoadInt64(&state.completed)
				failures := atomic.LoadInt64(&state.failures)
				rps := float64(completed) / elapsed.Seconds()

				state.mu.Lock()
				sorted := make([]float64, len(state.reservoir))
				copy(sorted, state.reservoir)
				state.mu.Unlock()
				sort.Float64s(sorted)
				p99 := "-"
				if len(sorted) > 0 {
					p99 = formatLatency(getPercentile(sorted, 0.99))
				}

				var progress string
				switch {
				case opts.infinite:
					progress = fmt.Sprintf("%d (%s)", completed, elapsed.Round(time.Millisecond))
				case opts.duration > 0:
					progress = fmt.Sprintf("%s/%s", elapsed.Round(time.Millisecond), opts.duration)
				default:
					progress = fmt.Sprintf("%d/%d", completed, opts.requests)
				}

				fmt.Fprintf(os.Stdout, "\r\033[K  %s  %d req/s  %d errors  p99: %s",
					progress, int64(math.Round(rps)), failures, p99)
			}
		}
	}()
	return func() {
		select {
		case <-stopCh:
		default:
			close(stopCh)
		}
	}
}

func isStdoutTTY() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// -- helpers -----------------------------------------------------------

func withTenant(tenant string) map[string]any {
	out := make(map[string]any, len(fixtureFunctionDefault))
	maps.Copy(out, fixtureFunctionDefault)
	out["tenant"] = tenant
	return out
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// formatLatency renders a millisecond latency value with tiered
// decimal precision: 2dp under 1ms, 1dp under 10ms, integer under 1s,
// and 1dp seconds beyond.
func formatLatency(ms float64) string {
	switch {
	case ms < 1:
		return fmt.Sprintf("%.2fms", ms)
	case ms < 10:
		return fmt.Sprintf("%.1fms", ms)
	case ms < 1000:
		return fmt.Sprintf("%dms", int64(math.Round(ms)))
	default:
		return fmt.Sprintf("%.1fs", ms/1000)
	}
}

// getPercentile computes a percentile (0..1) from a sorted slice using
// linear interpolation. Returns 0 for an empty slice.
func getPercentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	index := p * float64(len(sorted)-1)
	lower := int(math.Floor(index))
	upper := int(math.Ceil(index))
	if lower == upper {
		return sorted[lower]
	}
	weight := index - float64(lower)
	return sorted[lower] + (sorted[upper]-sorted[lower])*weight
}
