// Package e2e drives the in-process web UI through headless Chromium and
// asserts the URL-state behaviors the dashboard depends on. The suite
// boots a *web.Server seeded by webtest.New() at 127.0.0.1:8077 and
// relies on the project's preinstalled Chrome / Chromium binary; tests
// fail fast with a clear error if neither is available.
package e2e

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/gadget-inc/skipper/internal/web/webtest"
	"gotest.tools/v3/assert"
)

const baseURL = "http://127.0.0.1:8077"

var (
	serverErr  error
	serverOnce sync.Once
)

// browserContext returns a fresh chromedp context for one test, bounded
// by ctxTimeout. Each test gets its own browser tab so failures don't
// poison the shared allocator.
func browserContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", true),
			chromedp.Flag("disable-gpu", true),
			chromedp.NoSandbox,
		)...)

	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	ctx, cancelTimeout := context.WithTimeout(ctx, 30*time.Second)

	return ctx, func() {
		cancelTimeout()
		cancelCtx()
		cancelAlloc()
	}
}

func ensureServer(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("e2e suite needs headless Chrome; skipped under -short")
	}
	serverOnce.Do(func() {
		ln, err := net.Listen("tcp", "127.0.0.1:8077")
		if err != nil {
			serverErr = fmt.Errorf("bind 127.0.0.1:8077: %w", err)
			return
		}
		srv := webtest.New()
		go func() {
			if err := http.Serve(ln, srv.Handler()); err != nil && !errors.Is(err, http.ErrServerClosed) {
				fmt.Fprintln(os.Stderr, "e2e server stopped:", err)
			}
		}()
		serverErr = waitForReady(baseURL+"/healthz", 5*time.Second)
	})
	if serverErr != nil {
		t.Fatalf("e2e server failed to boot: %v", serverErr)
	}
}

func waitForReady(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("readiness probe %q did not respond OK within %s", url, timeout)
}

func TestFunctionsPagePrefillsSearchFromURL(t *testing.T) {
	ensureServer(t)
	ctx, cancel := browserContext(t)
	defer cancel()

	var inputValue, tableHTML string
	err := chromedp.Run(ctx,
		chromedp.Navigate(baseURL+"/functions?search=web"),
		chromedp.WaitVisible(`input[placeholder="Search functions…"]`, chromedp.ByQuery),
		chromedp.Value(`input[placeholder="Search functions…"]`, &inputValue, chromedp.ByQuery),
		chromedp.OuterHTML(`table tbody`, &tableHTML, chromedp.ByQuery),
	)
	assert.NilError(t, err)
	assert.Equal(t, inputValue, "web")
	assert.Assert(t, strings.Contains(tableHTML, "web-app"), "tbody missing web-app: %s", tableHTML)
	// Only the web-app row should be visible (other deployments contain "api-server"
	// and "worker", neither of which contain the substring "web").
	assert.Assert(t, !strings.Contains(tableHTML, "api-server"))
	assert.Assert(t, !strings.Contains(tableHTML, "worker"))
}

func TestSyncParamsKeyupUpdatesURL(t *testing.T) {
	ensureServer(t)
	ctx, cancel := browserContext(t)
	defer cancel()

	var url string
	err := chromedp.Run(ctx,
		chromedp.Navigate(baseURL+"/functions"),
		chromedp.WaitVisible(`input[placeholder="Search functions…"]`, chromedp.ByQuery),
		chromedp.Evaluate(`window.syncParams({ search: 'api', sort: 'deployment', dir: 'asc' })`, nil),
		chromedp.Location(&url),
	)
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(url, "search=api"), "url missing search=api: %s", url)
}

func TestSyncParamsSortClickUpdatesURL(t *testing.T) {
	ensureServer(t)
	ctx, cancel := browserContext(t)
	defer cancel()

	var url string
	err := chromedp.Run(ctx,
		chromedp.Navigate(baseURL+"/functions"),
		chromedp.WaitVisible(`input[placeholder="Search functions…"]`, chromedp.ByQuery),
		chromedp.Evaluate(`window.syncParams({ search: '', sort: 'namespace', dir: 'asc' })`, nil),
		chromedp.Location(&url),
	)
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(url, "sort=namespace"), "url missing sort=namespace: %s", url)
	// Empty values should be removed from the URL.
	assert.Assert(t, !strings.Contains(url, "search="), "url should drop empty search: %s", url)
}

func TestURLStatePreservedAcrossRefresh(t *testing.T) {
	ensureServer(t)
	ctx, cancel := browserContext(t)
	defer cancel()

	var beforeValue, afterValue, urlAfter string
	err := chromedp.Run(ctx,
		chromedp.Navigate(baseURL+"/functions?search=worker&sort=tenant&dir=desc"),
		chromedp.WaitVisible(`input[placeholder="Search functions…"]`, chromedp.ByQuery),
		chromedp.Value(`input[placeholder="Search functions…"]`, &beforeValue, chromedp.ByQuery),
		chromedp.Reload(),
		chromedp.WaitVisible(`input[placeholder="Search functions…"]`, chromedp.ByQuery),
		chromedp.Value(`input[placeholder="Search functions…"]`, &afterValue, chromedp.ByQuery),
		chromedp.Location(&urlAfter),
	)
	assert.NilError(t, err)
	assert.Equal(t, beforeValue, "worker")
	assert.Equal(t, afterValue, "worker")
	assert.Assert(t, strings.Contains(urlAfter, "search=worker"), "url missing search=worker: %s", urlAfter)
}

func TestTenantsPageSearchFromURL(t *testing.T) {
	ensureServer(t)
	ctx, cancel := browserContext(t)
	defer cancel()

	var inputValue string
	err := chromedp.Run(ctx,
		chromedp.Navigate(baseURL+"/tenants?search=tenant-1"),
		chromedp.WaitVisible(`input[placeholder="Search tenants…"]`, chromedp.ByQuery),
		chromedp.Value(`input[placeholder="Search tenants…"]`, &inputValue, chromedp.ByQuery),
	)
	assert.NilError(t, err)
	assert.Equal(t, inputValue, "tenant-1")
}

func TestEventsPageFunctionFilterFromURL(t *testing.T) {
	ensureServer(t)
	ctx, cancel := browserContext(t)
	defer cancel()

	var inputValue string
	err := chromedp.Run(ctx,
		chromedp.Navigate(baseURL+"/events?function=web"),
		chromedp.WaitVisible(`input[placeholder="Filter by function…"]`, chromedp.ByQuery),
		chromedp.Value(`input[placeholder="Filter by function…"]`, &inputValue, chromedp.ByQuery),
	)
	assert.NilError(t, err)
	assert.Equal(t, inputValue, "web")
}

func TestEventsPageSeverityFromURL(t *testing.T) {
	ensureServer(t)
	ctx, cancel := browserContext(t)
	defer cancel()

	var selectValue string
	err := chromedp.Run(ctx,
		chromedp.Navigate(baseURL+"/events?severity=1"),
		chromedp.WaitVisible(`select`, chromedp.ByQuery),
		chromedp.Value(`select`, &selectValue, chromedp.ByQuery),
	)
	assert.NilError(t, err)
	assert.Equal(t, selectValue, "1")
}
