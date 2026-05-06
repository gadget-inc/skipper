package docssite

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// errMessageUnknown is returned (wrapped) when a shortcode names a
// message not in messageTableRegistry. The wrap names the offending
// argument and lists the registered messages so authors can spot
// the typo.
var errMessageUnknown = errors.New("docssite: messageTable shortcode references unknown message")

// errMessageDescriptionDrift is returned (wrapped) when the
// description map disagrees with the proto descriptor: a missing
// `MessageName.field_name` entry, an extra entry, or a key whose
// `MessageName` is not in the registry.
var errMessageDescriptionDrift = errors.New("docssite: messageTable description drift")

// messageTableRegistry is the whitelist of proto messages the
// shortcode can render. Names match the bare proto message name
// (lookup against [protoreflect.MessageDescriptor.Name]).
//
// Empty request/response messages (`HeartbeatResponse`,
// `ReleaseInstanceResponse`, `GetClusterStateRequest`) are
// intentionally excluded -- they render as `**Response:** Empty.`
// prose in the docs, not via this shortcode.
var messageTableRegistry = []string{
	"Assignment",
	"Scale",
	"Instance",
	"Heartbeat",
	"GetInstanceRequest",
	"GetInstanceResponse",
	"HeartbeatRequest",
	"ScaleRequest",
	"ScaleResponse",
	"ReleaseInstanceRequest",
	"GetClusterStateResponse",
}

// messageTableDescriptions is the prose for each (Message, field)
// pair. Keys are `MessageName.proto_field_name` (the proto name,
// not the Go-camel name). The proto file does not carry these
// descriptions; the renderer's drift check enforces lockstep with
// the live descriptors at build time.
var messageTableDescriptions = map[string]string{
	"Assignment.namespace":  "Kubernetes namespace the assignment is scheduled in.",
	"Assignment.deployment": "Source deployment label that supplies the pod pool.",
	"Assignment.tenant":     "Tenant identifier; combined with namespace, deployment, and oneshot to compute the assignment hash.",
	"Assignment.metadata":   "Opaque string passed verbatim to the assigned pod; not part of the assignment hash.",
	"Assignment.scale":      "Per-assignment scaling targets (min, max, CPU, memory, in-flight requests).",
	"Assignment.oneshot":    "True if each request gets a fresh pod; assigned pods are released after the request completes.",

	"Scale.min_instances":             "Minimum ready-instance floor (0 enables scale-to-zero).",
	"Scale.max_instances":             "Hard ceiling on ready instances.",
	"Scale.target_cpu_usage_milli":    "Per-instance CPU target in millicores; the controller scales toward this average.",
	"Scale.target_memory_usage_mib":   "Per-instance memory target in mebibytes.",
	"Scale.target_in_flight_requests": "Per-instance in-flight-request target.",

	"Instance.assignment":       "Assignment this instance is bound to.",
	"Instance.name":             "Pod name in the cluster.",
	"Instance.addr":             "Backend address (`host:port`) the router proxies to.",
	"Instance.replica_set":      "Owning ReplicaSet name; used to detect stale instances after a rollout.",
	"Instance.assigned_at":      "When the controller atomically claimed the pod for this assignment.",
	"Instance.ready_at":         "When the pod confirmed assignment via `/__skipper/assign` and started serving traffic.",
	"Instance.cpu_usage_milli":  "Most recent CPU usage sample in millicores.",
	"Instance.memory_usage_mib": "Most recent memory usage sample in mebibytes.",

	"Heartbeat.assignment":         "Assignment the heartbeat is for.",
	"Heartbeat.timestamp":          "Sender's clock reading at the time the batch was assembled.",
	"Heartbeat.in_flight_requests": "In-flight request count at the time the batch was assembled.",

	"GetInstanceRequest.assignment":             "Assignment to get an instance for.",
	"GetInstanceRequest.exclude_instance_names": "Instance names to exclude; used by routers retrying after a failed dial.",

	"GetInstanceResponse.instance": "Assigned, ready instance.",

	"HeartbeatRequest.router_ip":     "IP of the router sending the heartbeat batch.",
	"HeartbeatRequest.heartbeats":    "One heartbeat per assignment with active in-flight requests.",
	"HeartbeatRequest.forwarded_for": "Controller IPs that have already processed this batch; used to break forwarding cycles.",

	"ScaleRequest.assignment":        "Assignment being scaled.",
	"ScaleRequest.desired_instances": "Target ready-instance count after scaling.",
	"ScaleRequest.reason":            "Reason the scale was triggered.",

	"ScaleResponse.instances": "Ready instances after the scale decision was applied.",

	"ReleaseInstanceRequest.instance": "Instance to release.",

	"GetClusterStateResponse.cluster_state": "Snapshot of the cluster's current supervisor, event, and config state.",
}

// renderMessageTable looks message up in messageTableRegistry,
// renders one row per field of the corresponding proto descriptor,
// and joins each row with the description from
// messageTableDescriptions. Returns errMessageUnknown for an
// unregistered name and errMessageDescriptionDrift for any
// disagreement between the descriptor and the description map.
func renderMessageTable(message string) (string, error) {
	return renderMessageRows(message, messageTableDescriptions, messageTableRegistry)
}

// renderMessageRows is the pure core of [renderMessageTable].
func renderMessageRows(message string, descriptions map[string]string, registry []string) (string, error) {
	if !slices.Contains(registry, message) {
		return "", fmt.Errorf("%w: %s (registered: %s)",
			errMessageUnknown, message, strings.Join(registry, ", "))
	}
	if err := checkMessageDescriptionDrift(descriptions, registry); err != nil {
		return "", err
	}
	desc, err := lookupMessageDescriptor(message)
	if err != nil {
		return "", err
	}
	if err := checkMessageFieldDescriptions(message, desc, descriptions); err != nil {
		return "", err
	}

	fields := desc.Fields()
	indices := make([]int, fields.Len())
	for i := range indices {
		indices[i] = i
	}
	sort.Slice(indices, func(i, j int) bool {
		return fields.Get(indices[i]).Number() < fields.Get(indices[j]).Number()
	})

	var b strings.Builder
	b.WriteString("| Field | Type | Number | Description |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, idx := range indices {
		f := fields.Get(idx)
		key := message + "." + string(f.Name())
		fmt.Fprintf(&b, "| `%s` | %s | %d | %s |\n",
			escapePipes(string(f.Name())),
			escapePipes(messageFieldTypeName(f)),
			f.Number(),
			escapePipes(descriptions[key]),
		)
	}
	return b.String(), nil
}

// checkMessageFieldDescriptions returns errMessageDescriptionDrift
// when (b) a field on the proto descriptor lacks a description, or
// (c) a description key MessageName.field_name names a field not
// present on the descriptor.
func checkMessageFieldDescriptions(message string, desc protoreflect.MessageDescriptor, descriptions map[string]string) error {
	fields := desc.Fields()
	descriptorFieldKeys := map[string]struct{}{}
	for i := range fields.Len() {
		key := message + "." + string(fields.Get(i).Name())
		descriptorFieldKeys[key] = struct{}{}
		if _, ok := descriptions[key]; !ok {
			return fmt.Errorf("%w: missing description for %s",
				errMessageDescriptionDrift, key)
		}
	}
	for key := range descriptions {
		prefix := message + "."
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if _, ok := descriptorFieldKeys[key]; !ok {
			return fmt.Errorf("%w: description key %s names a field not on the proto descriptor",
				errMessageDescriptionDrift, key)
		}
	}
	return nil
}

// checkMessageDescriptionDrift validates dimension (d): every
// description-map key's MessageName must be in the registry. The
// per-message field-vs-description check happens in
// [checkMessageFieldDescriptions].
func checkMessageDescriptionDrift(descriptions map[string]string, registry []string) error {
	for key := range descriptions {
		dot := strings.IndexByte(key, '.')
		if dot < 0 {
			return fmt.Errorf("%w: malformed description key %q (want MessageName.field_name)",
				errMessageDescriptionDrift, key)
		}
		messageName := key[:dot]
		if !slices.Contains(registry, messageName) {
			return fmt.Errorf("%w: description key %s names message %q not in registry (registered: %s)",
				errMessageDescriptionDrift, key, messageName, strings.Join(registry, ", "))
		}
	}
	return nil
}

// lookupMessageDescriptor maps a registered name to its
// [protoreflect.MessageDescriptor]. The proto reflection is grounded
// on a freshly-constructed message instance because the skipper
// proto file uses `edition = "2023"` with `API_OPAQUE`, and Go
// reflection over the generated structs would skip unexported
// fields.
func lookupMessageDescriptor(name string) (protoreflect.MessageDescriptor, error) {
	var msg proto.Message
	switch name {
	case "Assignment":
		msg = (&skipper.Assignment{})
	case "Scale":
		msg = (&skipper.Scale{})
	case "Instance":
		msg = (&skipper.Instance{})
	case "Heartbeat":
		msg = (&skipper.Heartbeat{})
	case "GetInstanceRequest":
		msg = (&skipper.GetInstanceRequest{})
	case "GetInstanceResponse":
		msg = (&skipper.GetInstanceResponse{})
	case "HeartbeatRequest":
		msg = (&skipper.HeartbeatRequest{})
	case "ScaleRequest":
		msg = (&skipper.ScaleRequest{})
	case "ScaleResponse":
		msg = (&skipper.ScaleResponse{})
	case "ReleaseInstanceRequest":
		msg = (&skipper.ReleaseInstanceRequest{})
	case "GetClusterStateResponse":
		msg = (&skipper.GetClusterStateResponse{})
	default:
		// renderMessageRows already gates on the registry; this
		// branch is defensive against a future registry edit that
		// adds a name without wiring the descriptor lookup.
		return nil, fmt.Errorf("%w: no descriptor wired for %s",
			errMessageUnknown, name)
	}
	return msg.ProtoReflect().Descriptor(), nil
}

// messageFieldTypeName renders a single field's type for the Type
// column. Scalar fields use the proto kind name (`string`,
// `uint32`, `bool`); enum and message-typed fields render their
// FullName with the local `skipper.` prefix stripped, so
// `skipper.Scale` becomes `Scale` while `google.protobuf.Timestamp`
// renders unchanged. Repeated fields receive a `repeated ` prefix.
func messageFieldTypeName(f protoreflect.FieldDescriptor) string {
	const localPrefix = "skipper."
	var name string
	switch f.Kind() {
	case protoreflect.MessageKind:
		name = strings.TrimPrefix(string(f.Message().FullName()), localPrefix)
	case protoreflect.EnumKind:
		name = strings.TrimPrefix(string(f.Enum().FullName()), localPrefix)
	default:
		name = f.Kind().String()
	}
	if f.IsList() {
		return "repeated " + name
	}
	return name
}
