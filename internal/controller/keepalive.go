package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
)

type TrafficEntry struct {
	Function    function.Function `json:"function"`
	LastRequest time.Time         `json:"lastRequest"`
}

func (c *Controller) handleTraffic(rw http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	var trafficEntries []TrafficEntry
	err = json.Unmarshal(body, &trafficEntries)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	c.fnTrafficMu.Lock()
	for _, trafficEntry := range trafficEntries {
		trafficEntry.Function.Metadata = "" // the idle function reaper doesn't have the function metadata, so we need to clear it to match the function in the map
		lastRequest, ok := c.fnTraffic[trafficEntry.Function]
		if !ok || trafficEntry.LastRequest.After(lastRequest) {
			c.fnTraffic[trafficEntry.Function] = trafficEntry.LastRequest
		}
	}
	defer c.fnTrafficMu.Unlock()

	log.Trace(req.Context(), "received traffic", slog.Int("trafficEntries", len(trafficEntries)))
	rw.WriteHeader(http.StatusOK)

	go func() {
		forwardedFor := req.Header.Values(key.ForwardedFor.Header)
		forwardedFor = append(forwardedFor, FlagIP.Value)

		for _, controllerIP := range c.ring.List() {
			if !slices.Contains(forwardedFor, controllerIP) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				controllerPort := strconv.Itoa(FlagPort.Value)
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+controllerIP+":"+controllerPort+"/traffic", bytes.NewBuffer(body))
				if err != nil {
					log.Warn(ctx, "failed to create traffic request", key.Error.Field(err))
					continue
				}

				req.Header.Set("Content-Type", "application/json")
				for _, forwardedForIP := range forwardedFor {
					req.Header.Add(key.ForwardedFor.Header, forwardedForIP)
				}

				log.Trace(ctx, "forwarding traffic", slog.String("controllerIP", controllerIP), key.ForwardedFor.Field(forwardedFor))
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					log.Warn(ctx, "failed to forward traffic", key.Error.Field(err))
					continue
				}

				if res.StatusCode != http.StatusOK {
					log.Warn(ctx, "forwarded traffic failed", slog.String("status", res.Status))
				}
			}
		}
	}()
}
