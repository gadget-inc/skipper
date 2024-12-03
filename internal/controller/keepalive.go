package controller

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	"github.com/goccy/go-json"
)

type KeepAlive struct {
	Function  function.Function `json:"function"`
	Timestamp time.Time         `json:"timestamp"`
}

func (c *Controller) handleKeepAlive(rw http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	var keepAlives []KeepAlive
	err = json.Unmarshal(body, &keepAlives)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	c.keepAlivesMu.Lock()
	for _, keepAlive := range keepAlives {
		keepAlive.Function.Metadata = "" // the idle function reaper doesn't have the function metadata, so we need to clear it to match the function in the map
		timestamp, ok := c.keepAlives[keepAlive.Function]
		if !ok || keepAlive.Timestamp.After(timestamp) {
			c.keepAlives[keepAlive.Function] = keepAlive.Timestamp
		}
	}
	c.keepAlivesMu.Unlock()

	log.Trace(req.Context(), "received keep alives", slog.Int("count", len(keepAlives)))
	rw.WriteHeader(http.StatusOK)

	go func() {
		forwardedFor := req.Header.Values(key.ForwardedFor.Header)
		forwardedFor = append(forwardedFor, FlagIP.Value)

		for _, controllerIP := range c.ring.List() {
			if !slices.Contains(forwardedFor, controllerIP) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				controllerPort := strconv.Itoa(FlagPort.Value)
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+controllerIP+":"+controllerPort+"/keepalive", bytes.NewBuffer(body))
				if err != nil {
					log.Warn(ctx, "failed to create keep alive request", key.Error.Field(err))
					continue
				}

				req.Header.Set("Content-Type", "application/json")
				for _, forwardedForIP := range forwardedFor {
					req.Header.Add(key.ForwardedFor.Header, forwardedForIP)
				}

				log.Trace(ctx, "forwarding keep alives", slog.String("controllerIP", controllerIP), key.ForwardedFor.Field(forwardedFor))
				res, err := http.DefaultClient.Do(req)
				if err != nil || res.StatusCode != http.StatusOK {
					log.Warn(ctx, "failed to forward keep alives", key.Error.Field(err))
					continue
				}
				res.Body.Close()

				if res.StatusCode != http.StatusOK {
					log.Warn(ctx, "failed to forward keep alives", slog.Int("statusCode", res.StatusCode))
				}
			}
		}
	}()
}
