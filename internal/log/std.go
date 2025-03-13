package log

import (
	"context"
	"io"
	stdlog "log"
)

type stdlogWriter struct {
	io.Writer
}

func (w stdlogWriter) Write(p []byte) (n int, err error) {
	Info(context.Background(), string(p))
	return len(p), nil
}

func StdLogger() *stdlog.Logger {
	return stdlog.New(stdlogWriter{}, "", 0)
}
