package controller

import (
	"sync"
	"testing"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/skipper"
	"gotest.tools/v3/assert"
)

func TestEventLog(t *testing.T) {
	t.Parallel()

	t.Run("add and snapshot", func(t *testing.T) {
		t.Parallel()

		el := &eventLog{}
		fn := fixture.NewFunction(t)

		el.add(fn, skipper.EventType_EVENT_TYPE_SCALE_UP, skipper.EventSeverity_EVENT_SEVERITY_INFO, "scaled up to 3")
		el.add(fn, skipper.EventType_EVENT_TYPE_SCALE_DOWN, skipper.EventSeverity_EVENT_SEVERITY_INFO, "scaled down to 1")

		events := el.snapshot()
		assert.Equal(t, len(events), 2)
		// newest first
		assert.Equal(t, events[0].GetMessage(), "scaled down to 1")
		assert.Equal(t, events[1].GetMessage(), "scaled up to 3")
	})

	t.Run("caps at 200 events", func(t *testing.T) {
		t.Parallel()

		el := &eventLog{}
		fn := fixture.NewFunction(t)

		for i := range 250 {
			el.add(fn, skipper.EventType_EVENT_TYPE_SCALE_UP, skipper.EventSeverity_EVENT_SEVERITY_INFO, "event "+string(rune('A'+i%26)))
		}

		events := el.snapshot()
		assert.Equal(t, len(events), maxEvents)
	})

	t.Run("newest first ordering", func(t *testing.T) {
		t.Parallel()

		el := &eventLog{}
		fn := fixture.NewFunction(t)

		el.add(fn, skipper.EventType_EVENT_TYPE_SCALE_UP, skipper.EventSeverity_EVENT_SEVERITY_INFO, "first")
		el.add(fn, skipper.EventType_EVENT_TYPE_SCALE_DOWN, skipper.EventSeverity_EVENT_SEVERITY_INFO, "second")
		el.add(fn, skipper.EventType_EVENT_TYPE_POD_ASSIGNED, skipper.EventSeverity_EVENT_SEVERITY_INFO, "third")

		events := el.snapshot()
		assert.Equal(t, events[0].GetMessage(), "third")
		assert.Equal(t, events[1].GetMessage(), "second")
		assert.Equal(t, events[2].GetMessage(), "first")
	})

	t.Run("empty snapshot", func(t *testing.T) {
		t.Parallel()

		el := &eventLog{}
		events := el.snapshot()
		assert.Equal(t, len(events), 0)
	})

	t.Run("thread safety", func(t *testing.T) {
		t.Parallel()

		el := &eventLog{}
		fn := fixture.NewFunction(t)

		var wg sync.WaitGroup
		for range 10 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range 50 {
					el.add(fn, skipper.EventType_EVENT_TYPE_SCALE_UP, skipper.EventSeverity_EVENT_SEVERITY_INFO, "concurrent")
				}
			}()
		}
		wg.Wait()

		events := el.snapshot()
		assert.Equal(t, len(events), maxEvents)
	})

	t.Run("preserves event fields", func(t *testing.T) {
		t.Parallel()

		el := &eventLog{}
		fn := fixture.NewFunction(t)

		el.add(fn, skipper.EventType_EVENT_TYPE_STUCK_INSTANCE_CLEANUP, skipper.EventSeverity_EVENT_SEVERITY_WARN, "stuck pod deleted")

		events := el.snapshot()
		assert.Equal(t, len(events), 1)
		assert.Equal(t, events[0].GetType(), skipper.EventType_EVENT_TYPE_STUCK_INSTANCE_CLEANUP)
		assert.Equal(t, events[0].GetSeverity(), skipper.EventSeverity_EVENT_SEVERITY_WARN)
		assert.Equal(t, events[0].GetMessage(), "stuck pod deleted")
		assert.Assert(t, events[0].HasTimestamp())
		assert.Equal(t, events[0].GetFunction().GetTenant(), fn.GetTenant())
	})
}
