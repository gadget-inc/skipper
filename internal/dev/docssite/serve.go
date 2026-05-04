package docssite

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/fsnotify/fsnotify"
)

// devServer hosts the markdown source tree over HTTP. Pages are
// rendered on demand instead of via a Build pass, the watcher
// publishes a reload signal on every .md change, and connected
// browsers refresh through a /__reload websocket.
type devServer struct {
	srcDir   string
	opts     ServeOptions
	renderer *renderInfo

	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
}

func newDevServer(srcDir string, opts ServeOptions) *devServer {
	return &devServer{
		srcDir:   srcDir,
		opts:     opts,
		renderer: newRenderer(opts.BuildOptions),
		clients:  map[*websocket.Conn]struct{}{},
	}
}

func (s *devServer) run(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/__reload", s.handleReload)
	mux.HandleFunc("/", s.handlePage)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	w, err := newSourceWatcher(s.srcDir)
	if err != nil {
		return fmt.Errorf("start watcher: %w", err)
	}
	defer w.Close()

	go w.run(ctx, s.broadcast)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// handlePage maps a request URL to a markdown source file under
// srcDir, renders it on the fly with live-reload turned on, and
// streams the result. Requests that don't resolve to a markdown file
// fall through to handleStatic, which serves any non-markdown file at
// the requested relative path.
func (s *devServer) handlePage(w http.ResponseWriter, r *http.Request) {
	rel := s.resolveSource(r.URL.Path)
	if rel == "" {
		s.handleStatic(w, r)
		return
	}
	full := filepath.Join(s.srcDir, rel)
	source, err := os.ReadFile(full)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	out, err := s.renderer.RenderPage(rel, source, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(out)
}

// handleStatic serves a non-markdown file from srcDir at the request
// path (less the configured BasePath). The handler refuses paths that
// escape srcDir and 404s anything else that doesn't resolve.
func (s *devServer) handleStatic(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Path
	prefix := strings.TrimRight(s.opts.BasePath, "/")
	if prefix != "" && strings.HasPrefix(reqPath, prefix) {
		reqPath = strings.TrimPrefix(reqPath, prefix)
	}
	clean := strings.TrimPrefix(reqPath, "/")
	if clean == "" || strings.Contains(clean, "..") {
		http.NotFound(w, r)
		return
	}
	full := filepath.Join(s.srcDir, clean)
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, full)
}

// resolveSource maps a request URL to a markdown file path relative
// to srcDir, mirroring htmlOutputPath's directory layout. Returns ""
// when no candidate source exists.
func (s *devServer) resolveSource(reqPath string) string {
	prefix := strings.TrimRight(s.opts.BasePath, "/")
	if prefix != "" && strings.HasPrefix(reqPath, prefix) {
		reqPath = strings.TrimPrefix(reqPath, prefix)
	}
	if reqPath == "" || reqPath == "/" {
		return firstExisting(s.srcDir, "index.md")
	}
	clean := strings.Trim(reqPath, "/")
	candidates := []string{
		clean + ".md",
		filepath.Join(clean, "index.md"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(s.srcDir, c)); err == nil {
			return c
		}
	}
	return ""
}

func firstExisting(root string, names ...string) string {
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(root, n)); err == nil {
			return n
		}
	}
	return ""
}

// handleReload upgrades the request to a websocket and tracks the
// connection until either side closes.
func (s *devServer) handleReload(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		return
	}
	s.mu.Lock()
	s.clients[conn] = struct{}{}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, conn)
		s.mu.Unlock()
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}()

	ctx := r.Context()
	for {
		if _, _, err := conn.Reader(ctx); err != nil {
			return
		}
	}
}

// broadcast sends a reload signal to every connected browser.
func (s *devServer) broadcast() {
	s.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(s.clients))
	for c := range s.clients {
		conns = append(conns, c)
	}
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, c := range conns {
		_ = c.Write(ctx, websocket.MessageText, []byte("reload"))
	}
}

// sourceWatcher wraps fsnotify with debouncing tuned for editors that
// emit multiple events per save.
type sourceWatcher struct {
	w *fsnotify.Watcher
}

func newSourceWatcher(root string) (*sourceWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return w.Add(path)
		}
		return nil
	}); err != nil {
		_ = w.Close()
		return nil, err
	}
	return &sourceWatcher{w: w}, nil
}

func (s *sourceWatcher) Close() error {
	return s.w.Close()
}

// run pumps fsnotify events, debounces, and calls onChange when a
// .md file under the watched tree changes. Blocks until ctx is done
// or the watcher closes.
func (s *sourceWatcher) run(ctx context.Context, onChange func()) {
	const debounce = 75 * time.Millisecond
	var timer *time.Timer
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-s.w.Events:
			if !ok {
				return
			}
			if !strings.HasSuffix(strings.ToLower(ev.Name), ".md") {
				continue
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(debounce, onChange)
		case _, ok := <-s.w.Errors:
			if !ok {
				return
			}
		}
	}
}
