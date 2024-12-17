package controller

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/hashring"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	"github.com/gadget-inc/fusion/internal/timer"
	"github.com/puzpuzpuz/xsync/v3"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

const (
	StatusPending    = "pending"
	StatusReady      = "ready"
	StatusUnassigned = "unassigned"
)

var (
	hasTenantRequirement labels.Requirement
	hasTenantSelector    labels.Selector

	doesNotHaveTenantRequirement labels.Requirement
	doesNotHaveTenantSelector    labels.Selector
)

func init() {
	hasTenant, err := labels.NewRequirement(key.Tenant.Label, selection.Exists, nil)
	if err != nil {
		panic(err)
	}

	hasTenantRequirement = *hasTenant
	hasTenantSelector = labels.NewSelector().Add(hasTenantRequirement)

	doesNotHaveTenant, err := labels.NewRequirement(key.Tenant.Label, selection.DoesNotExist, nil)
	if err != nil {
		panic(err)
	}

	doesNotHaveTenantRequirement = *doesNotHaveTenant
	doesNotHaveTenantSelector = labels.NewSelector().Add(doesNotHaveTenantRequirement)
}

type Heartbeat struct {
	Function  function.Function `json:"function"`
	Timestamp time.Time         `json:"timestamp"`
}

type Controller struct {
	ring              *hashring.HashRing
	clientset         kubernetes.Interface
	metricsClientset  metricsclientset.Interface
	podListerMap      map[string]podListerEntry
	controllerClients *xsync.MapOf[string, Client]
	scaleMu           *xsync.MapOf[function.Function, *sync.Mutex]
	heartbeats        map[function.Function]time.Time
	heartbeatsMu      sync.Mutex // guards heartbeats
}

func New(clientset kubernetes.Interface, metricsClient metricsclientset.Interface) *Controller {
	return &Controller{
		ring:              hashring.New(),
		clientset:         clientset,
		metricsClientset:  metricsClient,
		podListerMap:      make(map[string]podListerEntry, len(function.FlagNamespaces.Value())),
		controllerClients: xsync.NewMapOf[string, Client](),
		scaleMu:           xsync.NewMapOf[function.Function, *sync.Mutex](),
		heartbeats:        make(map[function.Function]time.Time),
	}
}

func (c *Controller) Start(ctx context.Context) error {
	err := c.startControllerInformer(ctx)
	if err != nil {
		return fmt.Errorf("failed to start controller pod informer: %w", err)
	}
	err = c.startPodInformers(ctx)
	if err != nil {
		return fmt.Errorf("failed to start managed pod informers: %w", err)
	}
	err = c.startReplicaSetInformer(ctx)
	if err != nil {
		return fmt.Errorf("failed to start managed replica set informer: %w", err)
	}
	err = c.startScalingInstances(ctx)
	if err != nil {
		return fmt.Errorf("failed to start scaling tenant pods: %w", err)
	}
	return nil
}

func (c *Controller) startScalingInstances(ctx context.Context) error {
	// TODO: garbage collect old stabilization windows
	stabilizationWindows := make(map[function.Function]*StabilizationWindow)

	go timer.Loop(
		ctx,
		15*time.Second,
		func(ctx context.Context) error {
			c.heartbeatsMu.Lock()
			heartbeats := maps.Clone(c.heartbeats)
			c.heartbeatsMu.Unlock()

			for _, namespace := range function.FlagNamespaces.Value() {
				functionMetrics, err := c.getFunctionMetrics(ctx, namespace)
				if err != nil {
					log.Warn(ctx, "failed to get function metrics", key.Error.Field(err))
					return nil
				}

				now := time.Now()
				for fn, instanceMetrics := range functionMetrics {
					timestamp, ok := heartbeats[fn]
					if !ok {
						log.Warn(ctx, "no heartbeat for function", key.Function.Field(fn))
						for _, instanceMetric := range instanceMetrics {
							if instanceMetric.AssignedAt.After(timestamp) {
								timestamp = instanceMetric.AssignedAt
							}
						}
					}

					if time.Since(timestamp) > 90*time.Second {
						delete(stabilizationWindows, fn)

						controllerIP := c.ring.Get(fn.RingKey())
						if controllerIP != FlagIP.Value() {
							log.Trace(ctx, "skipping scaling fn to 0, not assigned to this controller",
								key.Function.Field(fn),
								key.ControllerIP.Field(controllerIP),
								key.IP.Field(FlagIP.Value()),
								slog.Bool("ok", ok),
							)
							continue
						}

						log.Trace(ctx, "scaling function to 0", key.Function.Field(fn), key.Timestamp.Field(timestamp))
						_, err := c.scaleFunction(ctx, fn, 0)
						if err != nil {
							log.Warn(ctx, "failed to scale function", key.Error.Field(err), key.Function.Field(fn))
						}
						continue
					}

					currentInstances := len(instanceMetrics)
					desiredInstances, err := calculateDesiredInstances(
						currentInstances,
						instanceMetrics,
						int64(fn.TargetCPUUtilization),
						// int64(fn.TargetMemoryUtilization),
						DefaultConfig,
						now,
					)
					if err != nil {
						log.Trace(ctx, "failed to calculate desired instances", key.Error.Field(err), key.Function.Field(fn))
						continue
					}

					if desiredInstances < fn.MinInstances {
						desiredInstances = fn.MinInstances
					}

					if desiredInstances > fn.MaxInstances {
						desiredInstances = fn.MaxInstances
					}

					stabilizationWindow, exists := stabilizationWindows[fn]
					if !exists {
						stabilizationWindow = &StabilizationWindow{Window: DefaultConfig.DownscaleStabilization}
						stabilizationWindows[fn] = stabilizationWindow
					}

					log.Trace(ctx, "desired instances",
						key.Function.Field(fn),
						key.CurrentInstances.Field(currentInstances),
						key.DesiredInstances.Field(desiredInstances),
						key.MaxRecommendedInstances.Field(stabilizationWindow.GetMaxRecommendation()),
					)

					stabilizationWindow.RecordRecommendation(desiredInstances, now)

					controllerIP := c.ring.Get(fn.RingKey())
					if controllerIP != FlagIP.Value() {
						log.Trace(ctx, "skipping scaling for function, not assigned to this controller",
							key.Function.Field(fn),
							key.Controller.Field(controllerIP),
							key.IP.Field(FlagIP.Value()),
							slog.Bool("ok", ok),
						)
						continue
					}

					if desiredInstances < currentInstances {
						maxRecommendedInstances := stabilizationWindow.GetMaxRecommendation()
						if maxRecommendedInstances < currentInstances {
							desiredInstances = maxRecommendedInstances
						} else {
							desiredInstances = currentInstances
						}
					}

					if desiredInstances == 0 {
						// we only scale to 0 if the last request was more than 90 seconds ago
						log.Debug(ctx, "skipping scaling function to 0 based on hpa", key.Function.Field(fn))
						continue
					}

					log.Trace(ctx, "scaling function",
						key.Function.Field(fn),
						key.CurrentInstances.Field(currentInstances),
						key.DesiredInstances.Field(desiredInstances),
						key.MaxRecommendedInstances.Field(stabilizationWindow.GetMaxRecommendation()),
					)

					_, err = c.scaleFunction(ctx, fn, desiredInstances)
					if err != nil {
						log.Warn(ctx, "failed to scale function",
							key.Error.Field(err),
							key.Function.Field(fn),
							key.CurrentInstances.Field(currentInstances),
							key.DesiredInstances.Field(desiredInstances),
						)
					}
				}
			}

			return nil
		},
	)

	return nil
}
