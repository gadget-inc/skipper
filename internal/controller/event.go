package controller

import (
	"sync"

	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const maxEvents = 200

// eventLog is a thread-safe ring buffer for recent events.
type eventLog struct {
	mu     sync.Mutex
	events []*skipper.Event
}

func (el *eventLog) add(fn *skipper.Assignment, eventType skipper.EventType, severity skipper.EventSeverity, message string) {
	event := &skipper.Event{}
	event.SetTimestamp(timestamppb.Now())
	event.SetAssignment(fn)
	event.SetType(eventType)
	event.SetMessage(message)
	event.SetSeverity(severity)

	el.mu.Lock()
	defer el.mu.Unlock()

	el.events = append(el.events, event)
	if len(el.events) > maxEvents {
		trimmed := make([]*skipper.Event, maxEvents)
		copy(trimmed, el.events[len(el.events)-maxEvents:])
		el.events = trimmed
	}
}

// snapshot returns a copy of events in newest-first order.
func (el *eventLog) snapshot() []*skipper.Event {
	el.mu.Lock()
	defer el.mu.Unlock()

	// Reverse-fill: el.events is append-ordered (oldest first), result is newest first (index 0 = newest).
	result := make([]*skipper.Event, len(el.events))
	for i, event := range el.events {
		result[len(el.events)-1-i] = event
	}
	return result
}
