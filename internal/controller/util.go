package controller

import (
	"io"
	"net/http"
	"strings"
)

func getResponseBody(res *http.Response) string {
	bytes, err := io.ReadAll(res.Body)
	if err != nil {
		return "failed to read body"
	}
	return strings.TrimSpace(string(bytes))
}
