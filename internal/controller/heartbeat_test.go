package controller

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestHeartbeatTargets(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		ringIPs       []string
		forwardedFor  []string
		podIP         string
		wantTargets   []string
		wantForwarded []string
	}{
		{
			name:          "normal",
			ringIPs:       []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
			forwardedFor:  []string{"10.0.0.1"},
			podIP:         "10.0.0.2",
			wantTargets:   []string{"10.0.0.3"},
			wantForwarded: []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
		},
		{
			name:          "all seen",
			ringIPs:       []string{"10.0.0.1", "10.0.0.2"},
			forwardedFor:  []string{"10.0.0.1"},
			podIP:         "10.0.0.2",
			wantTargets:   []string{},
			wantForwarded: []string{"10.0.0.1", "10.0.0.2"},
		},
		{
			name:          "none seen",
			ringIPs:       []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
			forwardedFor:  []string{},
			podIP:         "10.0.0.4",
			wantTargets:   []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
			wantForwarded: []string{"10.0.0.4", "10.0.0.1", "10.0.0.2", "10.0.0.3"},
		},
		{
			name:          "empty ring",
			ringIPs:       []string{},
			forwardedFor:  []string{"10.0.0.1"},
			podIP:         "10.0.0.2",
			wantTargets:   []string{},
			wantForwarded: []string{"10.0.0.1", "10.0.0.2"},
		},
		{
			name:          "empty both",
			ringIPs:       []string{},
			forwardedFor:  []string{},
			podIP:         "10.0.0.1",
			wantTargets:   []string{},
			wantForwarded: []string{"10.0.0.1"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			targets, forwarded := heartbeatTargets(tc.ringIPs, tc.forwardedFor, tc.podIP)

			assert.DeepEqual(t, targets, tc.wantTargets)
			assert.DeepEqual(t, forwarded, tc.wantForwarded)
		})
	}
}

func BenchmarkHeartbeatTargets(b *testing.B) {
	ringIPs := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"}
	forwardedFor := []string{"10.0.0.1"}
	podIP := "10.0.0.2"

	b.ReportAllocs()
	for b.Loop() {
		heartbeatTargets(ringIPs, forwardedFor, podIP)
	}
}
