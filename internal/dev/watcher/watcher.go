// Package watcher provides a debounced fsnotify-based recursive
// directory watcher. The Watcher walks the root once on startup, adds
// every subdirectory to fsnotify, watches for new directories created
// later, and coalesces bursts of file changes into a single signal on
// the Events channel.
//
// fsnotify itself does not support recursive watching on Linux or macOS;
// the manual walk-and-add is the standard workaround.
package watcher

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Options configure a Watcher. The zero value is usable.
type Options struct {
	// Filter, if set, decides whether a file change should produce an
	// Events signal. It receives a path relative to the watch root.
	// Returning false drops the event silently.
	Filter func(rel string) bool

	// SkipDir, if set, decides whether a directory should be excluded
	// from the watch. Excluded directories are not added to fsnotify and
	// changes inside them never reach Filter. The path is relative to
	// the watch root.
	SkipDir func(rel string) bool

	// Debounce coalesces bursts of events into a single signal. Defaults
	// to 200ms.
	Debounce time.Duration
}

// Watcher reports recursive file-system changes under a root directory.
type Watcher struct {
	// Events fires once per debounce window when a relevant change is
	// observed. The channel closes when the watcher's context is
	// cancelled or its underlying fsnotify watcher errors.
	Events <-chan struct{}
}

// New starts a recursive watcher rooted at root. The watcher runs until
// ctx is cancelled, at which point Events is closed.
func New(ctx context.Context, root string, opts Options) (*Watcher, error) {
	if opts.Debounce == 0 {
		opts.Debounce = 200 * time.Millisecond
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err := addDirRecursive(fsw, root, root, opts.SkipDir); err != nil {
		fsw.Close()
		return nil, err
	}

	out := make(chan struct{}, 1)
	go run(ctx, fsw, root, opts, out)

	return &Watcher{Events: out}, nil
}

// NewGoWatcher returns a Watcher tuned for Go source: it filters to
// non-test `.go` files and skips the `tmp/` tree entirely.
func NewGoWatcher(ctx context.Context, root string) (*Watcher, error) {
	return New(ctx, root, Options{
		Filter: func(rel string) bool {
			if strings.HasSuffix(rel, "_test.go") {
				return false
			}
			return strings.HasSuffix(rel, ".go")
		},
		SkipDir: func(rel string) bool {
			if rel == "" || rel == "." {
				return false
			}
			parts := strings.Split(rel, string(filepath.Separator))
			if len(parts) == 0 {
				return false
			}
			switch parts[0] {
			case "tmp", "node_modules", ".git":
				return true
			}
			return false
		},
	})
}

func run(ctx context.Context, fsw *fsnotify.Watcher, root string, opts Options, out chan<- struct{}) {
	defer close(out)
	defer fsw.Close()

	var (
		timer   *time.Timer
		timerCh <-chan time.Time
	)

	armed := false
	arm := func() {
		if timer == nil {
			timer = time.NewTimer(opts.Debounce)
		} else if !armed {
			timer.Reset(opts.Debounce)
		}
		timerCh = timer.C
		armed = true
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-timerCh:
			armed = false
			timerCh = nil
			select {
			case out <- struct{}{}:
			default:
				// A signal is already buffered; coalesce.
			}
		case ev, ok := <-fsw.Events:
			if !ok {
				return
			}
			rel, err := filepath.Rel(root, ev.Name)
			if err != nil {
				continue
			}
			// New directory created — recurse into it so we catch its
			// future contents.
			if ev.Has(fsnotify.Create) {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					_ = addDirRecursive(fsw, ev.Name, root, opts.SkipDir)
				}
			}
			if opts.Filter != nil && !opts.Filter(rel) {
				continue
			}
			arm()
		case err, ok := <-fsw.Errors:
			if !ok {
				return
			}
			// fsnotify can emit transient errors (overflow, ENOSPC); we
			// keep going and rely on the next real event.
			_ = err
		}
	}
}

// addDirRecursive walks dir and registers every subdirectory with the
// fsnotify watcher. SkipDir is consulted with paths relative to root.
func addDirRecursive(fsw *fsnotify.Watcher, dir, root string, skipDir func(string) bool) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Permission errors on transient subdirectories shouldn't
			// crash the watcher — skip and continue.
			if errors.Is(err, fs.ErrPermission) {
				return nil
			}
			return err
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if skipDir != nil && skipDir(rel) {
			return filepath.SkipDir
		}
		if err := fsw.Add(path); err != nil {
			// Same rationale as above — directories can disappear
			// between WalkDir's enumeration and our Add call.
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		return nil
	})
}
