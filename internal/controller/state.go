package controller

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/apimachinery/pkg/labels"
)

// ClusterState collects a snapshot of the entire cluster state for the web UI.
func (ctrl *Controller) ClusterState(ctx context.Context) *skipper.ClusterState {
	state := &skipper.ClusterState{}
	state.SetPodIp(ctrl.config.PodIP)

	startedAt := ctrl.StartedAt()
	if !startedAt.IsZero() {
		state.SetStartedAt(timestamppb.New(startedAt))
	}

	state.SetControllerIps(ctrl.ring.List())

	var supervisors []*skipper.SupervisorState
	for _, sup := range ctrl.supervisors.All() {
		fn := sup.fn.Load()

		supState := &skipper.SupervisorState{}
		supState.SetFunction(fn)

		instances, err := ctrl.getInstances(ctx, fn)
		if err != nil {
			log.Warn(ctx, "failed to get instances for cluster state", key.Error.Slog(err), skipper.FunctionKey.Slog(fn))
		} else {
			supState.SetInstances(instances)
		}

		var heartbeats []*skipper.HeartbeatState
		for routerIP, hb := range sup.routerHeartbeats.All() {
			hbState := &skipper.HeartbeatState{}
			hbState.SetRouterIp(routerIP)
			hbState.SetHeartbeat(hb)
			heartbeats = append(heartbeats, hbState)
		}
		supState.SetRouterHeartbeats(heartbeats)

		supState.SetResponsibleControllerIp(ctrl.ring.Get(fn))

		// Active replica set
		activeRS := ctrl.getActiveReplicaSet(ctx, fn)
		if activeRS != "" {
			supState.SetActiveReplicaSet(activeRS)
		}

		supervisors = append(supervisors, supState)
	}

	state.SetSupervisors(supervisors)
	state.SetEvents(ctrl.events.snapshot())
	state.SetConfig(ctrl.configValues())

	return state
}

// getActiveReplicaSet returns the name of the newest replica set with Status.Replicas > 0
// for the given function's deployment.
func (ctrl *Controller) getActiveReplicaSet(ctx context.Context, fn *skipper.Function) string {
	namespaceLister, ok := ctrl.namespaceListers[fn.GetNamespace()]
	if !ok {
		log.Warn(ctx, "namespace not found in listers for active RS detection", skipper.FunctionKey.Slog(fn))
		return ""
	}

	// Lists all replica sets from the in-memory informer cache (no API call), then filters
	// by owner reference. O(replica_sets) per namespace but typically small and infrequent.
	replicaSets, err := namespaceLister.replicaSetLister.ReplicaSets(fn.GetNamespace()).List(labels.Everything())
	if err != nil {
		log.Warn(ctx, "failed to list replica sets for active RS detection", key.Error.Slog(err), skipper.FunctionKey.Slog(fn))
		return ""
	}

	var newestName string
	var newestTime int64

	for _, rs := range replicaSets {
		for _, ownerRef := range rs.OwnerReferences {
			if ownerRef.Kind == "Deployment" && ownerRef.Name == fn.GetDeployment() {
				if rs.Status.Replicas > 0 && rs.CreationTimestamp.UnixNano() > newestTime {
					newestTime = rs.CreationTimestamp.UnixNano()
					newestName = rs.Name
				}
			}
		}
	}

	return newestName
}

// configValues returns the controller configuration as a slice of ConfigValue protos,
// using reflection on Config struct tags. Sensitive fields are masked.
func (ctrl *Controller) configValues() []*skipper.ConfigValue {
	t := reflect.TypeFor[Config]()
	v := reflect.ValueOf(*ctrl.config)

	var values []*skipper.ConfigValue

	for i := range t.NumField() {
		field := t.Field(i)
		flagName := field.Tag.Get("flag")
		if flagName == "" {
			continue
		}

		description := field.Tag.Get("description")
		sensitive := field.Tag.Get("sensitive") == "true"

		var valueStr string
		if sensitive {
			valueStr = "****"
		} else {
			fieldVal := v.Field(i)
			if fieldVal.Kind() == reflect.Slice {
				elems := make([]string, fieldVal.Len())
				for j := range fieldVal.Len() {
					elems[j] = fmt.Sprintf("%v", fieldVal.Index(j).Interface())
				}
				valueStr = strings.Join(elems, ", ")
			} else {
				valueStr = fmt.Sprintf("%v", fieldVal.Interface())
			}
		}

		cv := &skipper.ConfigValue{}
		cv.SetName(flagName)
		cv.SetValue(valueStr)
		cv.SetDescription(description)
		values = append(values, cv)
	}

	return values
}
