package log

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestInit(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name  string
		cfg   Config
		check func(t *testing.T, dir string)
	}

	testCases := []testCase{
		{
			name: "no log file",
			cfg:  Config{Format: "json"},
			check: func(t *testing.T, dir string) {
				t.Helper()
				entries, err := os.ReadDir(dir)
				assert.NilError(t, err)
				assert.Equal(t, len(entries), 0)
			},
		},
		{
			name: "log file writes jsonl by default",
			cfg: Config{
				Format:  "json",
				LogFile: "out.jsonl",
			},
			check: func(t *testing.T, dir string) {
				t.Helper()
				data, err := os.ReadFile(filepath.Join(dir, "out.jsonl"))
				assert.NilError(t, err)
				content := string(data)
				assert.Assert(t, strings.Contains(content, `"msg":"hello"`), "got: %s", content)
				assert.Assert(t, strings.Contains(content, `"level":"INFO"`), "got: %s", content)
			},
		},
		{
			name: "log file level filters independently",
			cfg: Config{
				Format:       "json",
				Level:        LogLevel{slog.LevelDebug},
				LogFile:      "out.jsonl",
				LogFileLevel: "warn",
			},
			check: func(t *testing.T, dir string) {
				t.Helper()
				data, err := os.ReadFile(filepath.Join(dir, "out.jsonl"))
				assert.NilError(t, err)
				content := string(data)
				// INFO message should NOT appear — file level is WARN
				assert.Assert(t, !strings.Contains(content, `"msg":"hello"`), "info should be filtered, got: %s", content)
				// WARN message SHOULD appear — at or above file level
				assert.Assert(t, strings.Contains(content, `"msg":"warning"`), "warn should be written, got: %s", content)
			},
		},
		{
			name: "log file level inherits from main level",
			cfg: Config{
				Format:  "json",
				Level:   LogLevel{slog.LevelWarn},
				LogFile: "out.jsonl",
			},
			check: func(t *testing.T, dir string) {
				t.Helper()
				data, err := os.ReadFile(filepath.Join(dir, "out.jsonl"))
				assert.NilError(t, err)
				content := string(data)
				// INFO message should NOT appear — inherited WARN level
				assert.Assert(t, !strings.Contains(content, `"msg":"hello"`), "info should be filtered by inherited level, got: %s", content)
			},
		},
		{
			name: "log file format inherits from main format",
			cfg: Config{
				Format:  "text",
				LogFile: "out.log",
			},
			check: func(t *testing.T, dir string) {
				t.Helper()
				data, err := os.ReadFile(filepath.Join(dir, "out.log"))
				assert.NilError(t, err)
				content := string(data)
				assert.Assert(t, strings.Contains(content, "msg=hello"), "expected text format inherited from main, got: %s", content)
			},
		},
		{
			name: "log file format text",
			cfg: Config{
				Format:        "json",
				LogFile:       "out.log",
				LogFileFormat: "text",
			},
			check: func(t *testing.T, dir string) {
				t.Helper()
				data, err := os.ReadFile(filepath.Join(dir, "out.log"))
				assert.NilError(t, err)
				content := string(data)
				// text format uses key=value, not JSON
				assert.Assert(t, strings.Contains(content, "msg=hello"), "expected text format, got: %s", content)
				assert.Assert(t, !strings.Contains(content, `"msg"`), "should not be JSON, got: %s", content)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			cfg := tc.cfg
			if cfg.LogFile != "" {
				cfg.LogFile = filepath.Join(dir, cfg.LogFile)
			}

			handler, cleanup := buildHandler(&cfg)
			defer cleanup()
			logger := slog.New(handler)
			logger.Info("hello")
			logger.Warn("warning")

			tc.check(t, dir)
		})
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "valid json format",
			cfg:  Config{Format: "json"},
		},
		{
			name:    "invalid log format",
			cfg:     Config{Format: "xml"},
			wantErr: "invalid log format",
		},
		{
			name:    "invalid log file format",
			cfg:     Config{Format: "json", LogFile: "out.log", LogFileFormat: "xml"},
			wantErr: "invalid log file format",
		},
		{
			name:    "invalid log file level",
			cfg:     Config{Format: "json", LogFile: "out.log", LogFileLevel: "banana"},
			wantErr: "invalid log file level",
		},
		{
			name: "log file format defaults to main format",
			cfg:  Config{Format: "json", LogFile: "out.log"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				assert.NilError(t, err)
			} else {
				assert.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}
