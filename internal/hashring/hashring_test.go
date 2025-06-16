package hashring

import (
	"crypto/rand"
	"fmt"
	"math"
	mathrand "math/rand"
	"testing"
	"time"

	"github.com/shoenig/test/must"
)

// testKey implements RingKey interface for testing
type testKey string

func (k testKey) RingKey() string {
	return string(k)
}

func TestHashRing(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*HashRing)
		key      testKey
		wantIP   string
		wantList []string
	}{
		{
			name: "empty ring",
			setup: func(h *HashRing) {
				// No setup - empty ring
			},
			key:    "test-key",
			wantIP: "", // Will panic as per implementation
		},
		{
			name: "empty ring with wait time",
			setup: func(h *HashRing) {
				h.waitTime = 10 * time.Millisecond
				go func() {
					time.Sleep(time.Millisecond)
					h.Add("127.0.0.1")
				}()
			},
			key:    "test-key",
			wantIP: "127.0.0.1",
		},
		{
			name: "single IP",
			setup: func(h *HashRing) {
				h.Add("127.0.0.1")
			},
			key:      "test-key",
			wantIP:   "127.0.0.1",
			wantList: []string{"127.0.0.1"},
		},
		{
			name: "multiple IPs",
			setup: func(h *HashRing) {
				h.Add("127.0.0.1")
				h.Add("127.0.0.2")
				h.Add("127.0.0.3")
			},
			key:      "test-key",
			wantList: []string{"127.0.0.1", "127.0.0.2", "127.0.0.3"},
		},
		{
			name: "duplicate IP",
			setup: func(h *HashRing) {
				h.Add("127.0.0.1")
				h.Add("127.0.0.1") // Duplicate
			},
			key:      "test-key",
			wantIP:   "127.0.0.1",
			wantList: []string{"127.0.0.1"},
		},
		{
			name: "remove IP",
			setup: func(h *HashRing) {
				h.Add("127.0.0.1")
				h.Add("127.0.0.2")
				h.Remove("127.0.0.1")
			},
			key:      "test-key",
			wantIP:   "127.0.0.2",
			wantList: []string{"127.0.0.2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := New()
			tt.setup(h)

			// Test List() if wantList is specified
			if tt.wantList != nil {
				must.Eq(t, tt.wantList, h.List())
			}

			// Test Get() if wantIP is specified
			if tt.wantIP != "" {
				must.Eq(t, tt.wantIP, h.Get(tt.key))
			}

			// Test empty ring panic
			if tt.name == "empty ring" {
				defer func() {
					if r := recover(); r == nil {
						t.Error("Get() did not panic on empty ring")
					}
				}()
				h.Get(tt.key)
			}
		})
	}
}

func TestHashRingDistribution(t *testing.T) {
	const numKeys = 10000
	const allowedDeviationPercent = 11.0

	tests := []struct {
		name   string
		setup  func() *HashRing
		modify func(*HashRing)
	}{
		{
			name: "static distribution",
			setup: func() *HashRing {
				h := New()
				for range 4 {
					h.Add(randomInternalIP())
				}
				return h
			},
			modify: nil, // No modification
		},
		{
			name: "distribution after adding IPs",
			setup: func() *HashRing {
				h := New()
				// Start with 2 IPs
				for range 2 {
					h.Add(randomInternalIP())
				}
				return h
			},
			modify: func(h *HashRing) {
				// Add 3 more IPs
				for range 3 {
					h.Add(randomInternalIP())
				}
			},
		},
		{
			name: "distribution after removing IPs",
			setup: func() *HashRing {
				h := New()
				// Start with 5 IPs
				for range 5 {
					h.Add(randomInternalIP())
				}
				return h
			},
			modify: func(h *HashRing) {
				ips := h.List()
				// Remove 2 IPs
				h.Remove(ips[0])
				h.Remove(ips[2])
			},
		},
		{
			name: "distribution after adding and removing IPs",
			setup: func() *HashRing {
				h := New()
				// Start with 3 IPs
				for range 3 {
					h.Add(randomInternalIP())
				}
				return h
			},
			modify: func(h *HashRing) {
				// Remove one IP and add two new ones
				ips := h.List()
				h.Remove(ips[0])
				h.Add(randomInternalIP())
				h.Add(randomInternalIP())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup initial ring
			h := tt.setup()

			// Generate random keys
			keys := make([]testKey, numKeys)
			for i := range keys {
				keys[i] = testKey(rand.Text())
			}

			// First distribution check (before modification)
			checkDistribution(t, h, keys, allowedDeviationPercent)

			// Apply ring modification if specified
			if tt.modify != nil {
				tt.modify(h)

				// Check distribution again with the same keys
				checkDistribution(t, h, keys, allowedDeviationPercent)
			}
		})
	}
}

// checkDistribution tests if keys are evenly distributed across IPs
func checkDistribution(t *testing.T, h *HashRing, keys []testKey, allowedDeviationPercent float64) {
	distribution := make(map[string]int)

	// Map keys to IPs and count distribution
	for _, key := range keys {
		ip := h.Get(key)
		distribution[ip]++
	}

	// Calculate expected distribution
	ips := h.List()
	expected := float64(len(keys)) / float64(len(ips))
	allowedDeviation := expected * (allowedDeviationPercent / 100.0)

	// Check distribution
	for ip, count := range distribution {
		deviation := math.Abs(float64(count) - expected)
		if deviation > allowedDeviation {
			t.Errorf(
				"IP %s received %d keys (deviation %.2f%%), expected deviation less than %.2f%%",
				ip, count, (deviation/expected)*100, allowedDeviationPercent,
			)
		}
	}
}

func randomInternalIP() string {
	return fmt.Sprintf("10.36.%d.%d", mathrand.Intn(256), mathrand.Intn(256))
}
