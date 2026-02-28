package hashring

import (
	"maps"
	"slices"
	"sync/atomic"
	"time"

	"github.com/puzpuzpuz/xsync/v4"
)

// HashRing represents a thread-safe consistent hash ring of IPs.
type HashRing struct {
	ips          map[uint32]string        // Map from hash to ip
	hashes       []uint32                 // Sorted list of hashes
	sortedIPs    atomic.Pointer[[]string] // Cached List() result; nil means dirty
	mu           xsync.RBMutex            // Read-Write mutex to protect concurrent access
	virtualNodes int                      // Number of virtual nodes per IP
	waitTime     time.Duration            // Time to wait for the hash ring to be populated
}

type Hasher interface {
	Hash() uint64
}

type RingOption func(*HashRing)

func WithWaitTime(waitTime time.Duration) RingOption {
	return func(h *HashRing) {
		h.waitTime = waitTime
	}
}

// New creates a new HashRing.
//
// Example:
//
//	ring := New()
func New(opts ...RingOption) *HashRing {
	h := &HashRing{
		ips:          make(map[uint32]string),
		hashes:       []uint32{},
		virtualNodes: 1024, // Number of virtual nodes per IP, increase for better distribution
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

const (
	fnvOffset32 = uint32(2166136261)
	fnvPrime32  = uint32(16777619)
)

// nodeHash generates different hash values for the same node using FNV-1a
// for the input bytes followed by a murmur3 fmix32 avalanche finalizer.
func nodeHash(ip string, idx int) uint32 {
	// FNV-1a: hash ip + separator + index as raw big-endian uint16
	hash := fnvOffset32
	for i := range len(ip) {
		hash ^= uint32(ip[i])
		hash *= fnvPrime32
	}
	hash ^= uint32(':')
	hash *= fnvPrime32
	hash ^= uint32(byte(idx >> 8))
	hash *= fnvPrime32
	hash ^= uint32(byte(idx))
	hash *= fnvPrime32

	// murmur3 fmix32: proper avalanche finalizer
	hash ^= hash >> 16
	hash *= 0x85ebca6b
	hash ^= hash >> 13
	hash *= 0xc2b2ae35
	hash ^= hash >> 16

	return hash
}

// Add adds an IP to the hash ring.
//
// This method is safe for concurrent use by multiple goroutines.
//
// Example:
//
//	ring.Add("127.0.0.1")
func (h *HashRing) Add(ip string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Create multiple virtual nodes per IP
	for i := range h.virtualNodes {
		hash := nodeHash(ip, i)

		// Skip if this hash already exists
		if _, exists := h.ips[hash]; exists {
			continue
		}

		// Map the hash to the IP
		h.ips[hash] = ip

		// Append the hash to the list of hashes
		h.hashes = append(h.hashes, hash)
	}

	// Sort the hashes to maintain the ring order
	slices.Sort(h.hashes)

	// Invalidate the cached List() result
	h.sortedIPs.Store(nil)
}

// Remove removes an IP from the hash ring.
//
// This method is safe for concurrent use by multiple goroutines.
//
// Example:
//
//	ring.Remove("127.0.0.1")
func (h *HashRing) Remove(ip string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Collect all hashes for this IP
	var hashesToRemove []uint32
	for hash, storedIP := range h.ips {
		if storedIP == ip {
			hashesToRemove = append(hashesToRemove, hash)
		}
	}

	// Remove all hashes for this IP
	for _, hash := range hashesToRemove {
		delete(h.ips, hash)

		// Find and remove the hash from the sorted list
		index, found := slices.BinarySearch(h.hashes, hash)
		if found {
			h.hashes = slices.Delete(h.hashes, index, index+1)
		}
	}

	// Invalidate the cached List() result
	h.sortedIPs.Store(nil)
}

// Get returns the IP responsible for the given key.
//
// This method is safe for concurrent use by multiple goroutines.
//
// Example:
//
//	ip := ring.Get("my-cache-key")
func (h *HashRing) Get(value Hasher) string {
	rt := h.mu.RLock()

	if len(h.hashes) == 0 {
		h.mu.RUnlock(rt)

		// Wait for the hash ring to be populated
		waitAttempts := 4
		for range waitAttempts {
			time.Sleep(h.waitTime / time.Duration(waitAttempts))
			rt = h.mu.RLock()
			if len(h.hashes) > 0 {
				break
			}
			h.mu.RUnlock(rt)
		}
		if len(h.hashes) == 0 {
			panic("hash ring is empty")
		}
	}

	defer h.mu.RUnlock(rt)

	// XOR-fold the 64-bit hash to 32-bit for the ring
	v := value.Hash()
	hash := uint32(v ^ (v >> 32))

	// Use binary search to find the first hash that is greater than or equal to the key's hash
	index, found := slices.BinarySearch(h.hashes, hash)
	if !found {
		// If we didn't find an exact match, index will be where the hash would be inserted
		if index == len(h.hashes) {
			// If the hash would be inserted at the end, wrap around to the beginning
			index = 0
		}
	}

	ipHash := h.hashes[index]

	return h.ips[ipHash]
}

// List returns a sorted slice of all the IPs in the hash ring.
//
// This method is safe for concurrent use by multiple goroutines.
// The result is cached and invalidated when the ring is mutated.
func (h *HashRing) List() []string {
	if cached := h.sortedIPs.Load(); cached != nil {
		return *cached
	}

	rt := h.mu.RLock()
	defer h.mu.RUnlock(rt)

	// Double-check after acquiring the read lock.
	if cached := h.sortedIPs.Load(); cached != nil {
		return *cached
	}

	result := slices.Compact(slices.Sorted(maps.Values(h.ips)))
	h.sortedIPs.Store(&result)
	return result
}
