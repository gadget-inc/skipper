package hashring

import (
	"hash/crc32"
	"hash/fnv"
	"maps"
	"slices"
	"strconv"

	"github.com/puzpuzpuz/xsync/v4"
)

// HashRing represents a thread-safe consistent hash ring of IPs.
type HashRing struct {
	ips          map[uint32]string // Map from hash to ip
	hashes       []uint32          // Sorted list of hashes
	mu           xsync.RBMutex     // Read-Write mutex to protect concurrent access
	virtualNodes int               // Number of virtual nodes per IP
}

type RingKey interface {
	RingKey() string
}

// New creates a new HashRing.
//
// Example:
//
//	ring := New()
func New() *HashRing {
	return &HashRing{
		ips:          make(map[uint32]string),
		hashes:       []uint32{},
		virtualNodes: 4096, // Number of virtual nodes per IP, increase for better distribution
	}
}

// getNodeHash generates different hash values for the same node
// using the FNV hash and CRC32 hash to create better distribution
func (h *HashRing) getNodeHash(ip string, idx int) uint32 {
	// Use FNV hash for the key with a virtual node number
	key := ip + ":" + strconv.Itoa(idx)

	// Generate primary hash using FNV
	fnvHash := fnv.New32a()
	fnvHash.Write([]byte(key))
	primaryHash := fnvHash.Sum32()

	// Generate the final hash using CRC32
	keyWithHash := key + ":" + strconv.FormatUint(uint64(primaryHash), 16)
	return crc32.ChecksumIEEE([]byte(keyWithHash))
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
		hash := h.getNodeHash(ip, i)

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
}

// Get returns the IP responsible for the given key.
//
// This method is safe for concurrent use by multiple goroutines.
//
// Example:
//
//	ip := ring.Get("my-cache-key")
func (h *HashRing) Get(value RingKey) string {
	if len(h.hashes) == 0 {
		panic("hash ring is empty")
	}

	rt := h.mu.RLock()
	defer h.mu.RUnlock(rt)

	// Compute the hash of the key
	hash := crc32.ChecksumIEEE([]byte(value.RingKey()))

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
func (h *HashRing) List() []string {
	rt := h.mu.RLock()
	defer h.mu.RUnlock(rt)
	return slices.Compact(slices.Sorted(maps.Values(h.ips)))
}
