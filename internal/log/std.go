package log

import (
	"bytes"
	"context"
	"io"
	stdlog "log"
)

type stdlogWriter struct {
	io.Writer
}

func (w stdlogWriter) Write(p []byte) (n int, err error) {
	p = bytes.TrimSuffix(p, []byte("\n")) // remove the trailing newline
	Info(context.Background(), string(p))
	return len(p), nil
}

func StdLogger() *stdlog.Logger {
	return stdlog.New(stdlogWriter{}, "", 0)
}
