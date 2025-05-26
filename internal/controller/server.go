package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var heartbeatsCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "skipper",
	Subsystem: "controller",
	Name:      "heartbeats_total",
	Help:      "The number of heartbeats received by the controller",
}, []string{"function_deployment"})
