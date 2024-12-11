package controller

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	"github.com/goccy/go-json"
)

type Heartbeat struct {
	Function  function.Function `json:"function"`
	Timestamp time.Time         `json:"timestamp"`
}

func (c *Controller) handleHeartbeat(rw http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	var heartbeats []Heartbeat
	err = json.Unmarshal(body, &heartbeats)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	c.heartbeatsMu.Lock()
	for _, heartbeat := range heartbeats {
		heartbeat.Function.Metadata = "" // the idle function reaper doesn't have the function metadata, so we need to clear it to match the function in the map
		timestamp, ok := c.heartbeats[heartbeat.Function]
		if !ok || heartbeat.Timestamp.After(timestamp) {
			c.heartbeats[heartbeat.Function] = heartbeat.Timestamp
		}
	}
	c.heartbeatsMu.Unlock()

	log.Trace(req.Context(), "received heartbeats", key.Count.Field(len(heartbeats)))
	rw.WriteHeader(http.StatusOK)

	go func() {
		forwardedFor := req.Header.Values(key.ForwardedFor.Header)
		forwardedFor = append(forwardedFor, FlagIP.Value())

		for _, controllerIP := range c.ring.List() {
			if !slices.Contains(forwardedFor, controllerIP) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				controllerPort := strconv.Itoa(FlagPort.Value())
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+controllerIP+":"+controllerPort+"/heartbeat", bytes.NewBuffer(body))
				if err != nil {
					log.Warn(ctx, "failed to create heartbeat request", key.Error.Field(err))
					continue
				}

				req.Header.Set("Content-Type", "application/json")
				for _, forwardedForIP := range forwardedFor {
					req.Header.Add(key.ForwardedFor.Header, forwardedForIP)
				}

				log.Trace(ctx, "forwarding heartbeats", key.ControllerIP.Field(controllerIP), key.ForwardedFor.Field(forwardedFor))
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					log.Warn(ctx, "failed to forward heartbeats", key.Error.Field(err))
					continue
				}
				res.Body.Close()

				if res.StatusCode != http.StatusOK {
					log.Warn(ctx, "failed to forward heartbeats", key.StatusCode.Field(res.StatusCode), key.Body.Field(getResponseBody(res)))
				}
			}
		}
	}()
}
