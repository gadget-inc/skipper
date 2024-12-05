package controller

import (
	"math/rand"
	"net/http"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	"github.com/goccy/go-json"
)

func (c *Controller) handleGet(rw http.ResponseWriter, req *http.Request) {
	fn, err := function.FromHeaders(req)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	instances, err := c.getAssigned(fn)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}

	for len(instances) == 0 {
		instances, err = c.scaleFunction(req.Context(), fn, 1)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	instance := instances[rand.Intn(len(instances))]

	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(http.StatusOK)
	err = json.NewEncoder(rw).Encode(instance)
	if err != nil {
		log.Error(req.Context(), "failed to encode get response", key.Error.Field(err))
	}
}
