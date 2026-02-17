package hashring

import (
	"crypto/rand"
	"fmt"
	"math"
	mathrand "math/rand"
	"testing"
	"time"

	"github.com/cespare/xxhash/v2"
	"gotest.tools/v3/assert"
)

// testKey implements Hasher interface for testing
type testKey string

func (k testKey) Hash() uint64 {
	return xxhash.Sum64String(string(k))
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
				assert.DeepEqual(t, h.List(), tt.wantList)
			}

			// Test Get() if wantIP is specified
			if tt.wantIP != "" {
				assert.Assert(t, h.Get(tt.key) == tt.wantIP)
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

// checkDistribution tests if keys are evenly distributed across IPs and logs
// a chi-squared statistic for comparing distribution quality across hash changes.
func checkDistribution(t *testing.T, h *HashRing, keys []testKey, allowedDeviationPercent float64) {
	t.Helper()

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

	// Chi-squared statistic: sum((observed - expected)^2 / expected)
	var chiSquared float64
	for _, count := range distribution {
		diff := float64(count) - expected
		chiSquared += (diff * diff) / expected
	}
	t.Logf("chi-squared = %.2f (df=%d, expected=%.1f per IP)", chiSquared, len(ips)-1, expected)

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

func TestNodeHashAvalanche(t *testing.T) {
	// Flip a single bit in idx, verify each output bit flips ~50% of the time.
	// Perfect avalanche: every input bit change flips each output bit with 50% probability.
	const (
		trials = 10000
		minPct = 30.0
		maxPct = 70.0
	)

	ip := "10.0.0.1"
	bitFlips := [32]int{} // count how many times each output bit flipped

	for i := range trials {
		base := nodeHash(ip, i)
		// Flip the lowest bit of idx
		flipped := nodeHash(ip, i^1)
		diff := base ^ flipped
		for bit := range 32 {
			if diff&(1<<bit) != 0 {
				bitFlips[bit]++
			}
		}
	}

	for bit, count := range bitFlips {
		pct := float64(count) / float64(trials) * 100
		if pct < minPct || pct > maxPct {
			t.Errorf("bit %d flipped %.1f%% of trials (want %.0f-%.0f%%)", bit, pct, minPct, maxPct)
		}
	}
}

func TestNodeHashCollisions(t *testing.T) {
	// Generate hashes for multiple IPs × 1024 virtual nodes and count collisions.
	tests := []struct {
		name          string
		numIPs        int
		maxCollisions int
	}{
		{"4-ips", 4, 0},
		{"10-ips", 10, 2},
		{"20-ips", 20, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seen := make(map[uint32]struct{})
			collisions := 0
			for i := range tt.numIPs {
				ip := fmt.Sprintf("10.0.%d.%d", i/256, i%256+1)
				for idx := range 1024 {
					h := nodeHash(ip, idx)
					if _, exists := seen[h]; exists {
						collisions++
					}
					seen[h] = struct{}{}
				}
			}
			t.Logf("collisions: %d / %d hashes", collisions, tt.numIPs*1024)
			if collisions > tt.maxCollisions {
				t.Errorf("got %d collisions, want at most %d", collisions, tt.maxCollisions)
			}
		})
	}
}

var sinkIP string

func BenchmarkHashRingGet(b *testing.B) {
	ips4 := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"}
	ips20 := make([]string, 20)
	for i := range ips20 {
		ips20[i] = fmt.Sprintf("10.0.%d.%d", i/256, i%256+1)
	}
	key := testKey("benchmark-key")

	b.Run("4-ips", func(b *testing.B) {
		ring := New()
		for _, ip := range ips4 {
			ring.Add(ip)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			sinkIP = ring.Get(key)
		}
	})

	b.Run("20-ips", func(b *testing.B) {
		ring := New()
		for _, ip := range ips20 {
			ring.Add(ip)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			sinkIP = ring.Get(key)
		}
	})

	b.Run("4-ips/parallel", func(b *testing.B) {
		ring := New()
		for _, ip := range ips4 {
			ring.Add(ip)
		}
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_ = ring.Get(key)
			}
		})
	})

	b.Run("20-ips/parallel", func(b *testing.B) {
		ring := New()
		for _, ip := range ips20 {
			ring.Add(ip)
		}
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_ = ring.Get(key)
			}
		})
	})
}

var sinkHash uint32

func BenchmarkGetNodeHash(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		sinkHash = nodeHash("10.0.0.1", 2048)
	}
}

func BenchmarkHashRingAdd(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		ring := New()
		ring.Add("10.0.0.1")
	}
}

func randomInternalIP() string {
	return fmt.Sprintf("10.36.%d.%d", mathrand.Intn(256), mathrand.Intn(256))
}
