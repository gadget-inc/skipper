package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/url"
	"strings"
	"time"

	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// signalsAttr renders an alternating key/value list as a JSON object suitable
// for a Datastar `data-signals` attribute. The JSON encoder properly escapes
// quotes, backslashes, and HTML-meaningful characters in user-controlled
// strings, so the resulting expression survives an HTML attribute round-trip.
func signalsAttr(pairs ...any) (template.JS, error) {
	if len(pairs)%2 != 0 {
		return "", fmt.Errorf("signals: expected even number of arguments, got %d", len(pairs))
	}
	obj := make(map[string]any, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			return "", fmt.Errorf("signals: argument %d is not a string key", i)
		}
		obj[key] = pairs[i+1]
	}
	encoded, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	return template.JS(encoded), nil
}

func timeAgo(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return "—"
	}
	seconds := int(time.Since(ts.AsTime()).Seconds())
	if seconds < 0 {
		return "just now"
	}
	if seconds < 60 {
		return fmt.Sprintf("%ds ago", seconds)
	}
	minutes := seconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%dm ago", minutes)
	}
	hours := minutes / 60
	if hours < 24 {
		return fmt.Sprintf("%dh ago", hours)
	}
	days := hours / 24
	return fmt.Sprintf("%dd ago", days)
}

func formatTimestamp(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return "—"
	}
	return ts.AsTime().UTC().Format(time.RFC3339)
}

func durationBetween(start, end *timestamppb.Timestamp) string {
	if start == nil || end == nil {
		return "—"
	}
	ms := end.AsTime().Sub(start.AsTime()).Milliseconds()
	if ms < 0 {
		return "—"
	}
	seconds := int(ms / 1000)
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	remaining := seconds % 60
	if remaining == 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%dm %ds", minutes, remaining)
}

func assignmentKey(fn *skipper.Assignment) string {
	if fn == nil {
		return ""
	}
	return fn.GetNamespace() + ":" + fn.GetDeployment() + ":" + fn.GetTenant()
}

func functionPath(fn *skipper.Assignment) string {
	return "/functions/" + url.PathEscape(assignmentKey(fn))
}

func scaleReasonLabel(reason skipper.ScaleReason) string {
	switch reason {
	case skipper.ScaleReason_SCALE_REASON_CPU:
		return "cpu"
	case skipper.ScaleReason_SCALE_REASON_HEARTBEAT_TIMEOUT:
		return "heartbeat_timeout"
	case skipper.ScaleReason_SCALE_REASON_IN_FLIGHT_REQUESTS:
		return "in_flight_requests"
	case skipper.ScaleReason_SCALE_REASON_MEMORY:
		return "memory"
	case skipper.ScaleReason_SCALE_REASON_NO_READY_INSTANCES:
		return "no_ready_instances"
	default:
		return "unspecified"
	}
}

func eventTypeLabel(t skipper.EventType) string {
	switch t {
	case skipper.EventType_EVENT_TYPE_SCALE_UP:
		return "scale up"
	case skipper.EventType_EVENT_TYPE_SCALE_DOWN:
		return "scale down"
	case skipper.EventType_EVENT_TYPE_POD_ASSIGNED:
		return "pod assigned"
	case skipper.EventType_EVENT_TYPE_POD_DELETED:
		return "pod deleted"
	case skipper.EventType_EVENT_TYPE_STALE_REPLACEMENT:
		return "stale replacement"
	case skipper.EventType_EVENT_TYPE_HEARTBEAT_TIMEOUT:
		return "heartbeat timeout"
	case skipper.EventType_EVENT_TYPE_STUCK_INSTANCE_CLEANUP:
		return "stuck instance cleanup"
	default:
		return "unspecified"
	}
}

func eventSeverityBadge(s skipper.EventSeverity) template.HTML {
	switch s {
	case skipper.EventSeverity_EVENT_SEVERITY_WARN:
		return `<span class="badge badge-yellow">warn</span>`
	case skipper.EventSeverity_EVENT_SEVERITY_INFO:
		return `<span class="badge badge-muted">info</span>`
	default:
		return `<span class="badge badge-muted">unknown</span>`
	}
}

func statusBadge(ready bool) template.HTML {
	if ready {
		return `<span class="badge badge-green">ready</span>`
	}
	return `<span class="badge badge-yellow">pending</span>`
}

func instanceState(inst *skipper.Instance, activeRS string) string {
	if activeRS != "" && inst.GetReplicaSet() != activeRS {
		return "stale"
	}
	if inst.HasReadyAt() {
		return "ready"
	}
	return "pending"
}

func instanceStateBadge(inst *skipper.Instance, activeRS string) template.HTML {
	state := instanceState(inst, activeRS)
	switch state {
	case "ready":
		return `<span class="badge badge-green">ready</span>`
	case "stale":
		return `<span class="badge badge-yellow">stale</span>`
	default:
		return `<span class="badge badge-yellow">pending</span>`
	}
}

func metricsString(metrics []*skipper.ScaleMetric) string {
	if len(metrics) == 0 {
		return "—"
	}
	parts := make([]string, len(metrics))
	for i, m := range metrics {
		parts[i] = fmt.Sprintf("%s: %.1f", m.GetName(), m.GetValue())
	}
	return strings.Join(parts, ", ")
}

func pct(a, b int) int {
	if b == 0 {
		return 0
	}
	return a * 100 / b
}

// activeNav returns the nav path that should be highlighted for the given page title.
func activeNav(title string) string {
	lower := strings.ToLower(title)
	switch {
	case strings.HasPrefix(lower, "function"):
		return "/functions"
	case strings.HasPrefix(lower, "controller"):
		return "/controllers"
	case strings.HasPrefix(lower, "tenant"):
		return "/tenants"
	case strings.HasPrefix(lower, "router"):
		return "/routers"
	case strings.HasPrefix(lower, "instance"):
		return "/functions"
	case strings.HasPrefix(lower, "event"):
		return "/events"
	case strings.HasPrefix(lower, "deployment"):
		return "/deployments"
	case strings.HasPrefix(lower, "config"):
		return "/config"
	default:
		return "/"
	}
}
