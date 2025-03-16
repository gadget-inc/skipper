package log

import (
	"bytes"
	"context"
	"io"
	stdlog "log"
	"log/slog"
)

type stdlogWriter struct {
	io.Writer
	level slog.Level
}

func (w stdlogWriter) Write(p []byte) (n int, err error) {
	p = bytes.TrimSuffix(p, []byte("\n")) // remove the trailing newline
	Log(context.Background(), w.level, string(p))
	return len(p), nil
}

func StdLogger(level slog.Level) *stdlog.Logger {
	return stdlog.New(stdlogWriter{level: level}, "", 0)
}
