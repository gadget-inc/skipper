package controller

import (
	"io"
	"net/http"
	"strings"
)

func panicIf(err error) {
	if err != nil {
		panic(err)
	}
}

func unwrap[V any](v V, err error) V {
	panicIf(err)
	return v
}

func getResponseBody(res *http.Response) string {
	bytes, err := io.ReadAll(res.Body)
	if err != nil {
		return "failed to read body"
	}
	return strings.TrimSpace(string(bytes))
}
