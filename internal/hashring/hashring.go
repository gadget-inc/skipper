package hashring

import (
	"hash/crc32"
	"slices"
	"sort"

	"github.com/puzpuzpuz/xsync/v3"
)

// curtesy of gpt-01-preview :)

// HashRing represents a thread-safe consistent hash ring of IPs.
type HashRing struct {
	ips    map[uint32]string // Map from hash to ip
	hashes []uint32          // Sorted list of hashes
	mu     xsync.RBMutex     // Read-Write mutex to protect concurrent access
}

// New creates a new HashRing.
//
// Example:
//
//	ring := New()
func New() *HashRing {
	return &HashRing{
		ips:    make(map[uint32]string),
		hashes: []uint32{},
	}
}

// Add adds an IP to the hash ring.
//
// This method is safe for concurrent use by multiple goroutines.
//
// Example:
//
//	ring.Add("127.0.0.1")
func (h *HashRing) Add(ip string) {
	// Compute the hash of the ip
	hash := crc32.ChecksumIEEE([]byte(ip))

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if the ip already exists
	if _, exists := h.ips[hash]; exists {
		return
	}

	// Map the hash to the ip
	h.ips[hash] = ip

	// Append the hash to the list of hashes
	h.hashes = append(h.hashes, hash)

	// Sort the hashes to maintain the ring order
	sort.Slice(h.hashes, func(i, j int) bool { return h.hashes[i] < h.hashes[j] })
}

// Remove removes an IP from the hash ring.
//
// This method is safe for concurrent use by multiple goroutines.
//
// Example:
//
//	ring.Remove("127.0.0.1")
func (h *HashRing) Remove(ip string) {
	// Compute the hash of the ip
	hash := crc32.ChecksumIEEE([]byte(ip))

	h.mu.Lock()
	defer h.mu.Unlock()

	// Remove the ip from the map
	delete(h.ips, hash)

	// Remove hash from the sorted list of hashes
	index := sort.Search(len(h.hashes), func(i int) bool { return h.hashes[i] >= hash })
	if index < len(h.hashes) && h.hashes[index] == hash {
		h.hashes = append(h.hashes[:index], h.hashes[index+1:]...)
	}
}

// Get returns the IP responsible for the given key.
//
// This method is safe for concurrent use by multiple goroutines.
//
// Example:
//
//	ip := ring.Get("my-cache-key")
func (h *HashRing) Get(key string) (string, bool) {
	if len(h.hashes) == 0 {
		return "", false
	}

	rt := h.mu.RLock()
	defer h.mu.RUnlock(rt)

	// Compute the hash of the key
	hash := crc32.ChecksumIEEE([]byte(key))

	// Locate the nearest ip greater than or equal to the key's hash
	index := sort.Search(len(h.hashes), func(i int) bool { return h.hashes[i] >= hash })
	if index == len(h.hashes) {
		// Wrap around to the first ip
		index = 0
	}

	ipHash := h.hashes[index]

	return h.ips[ipHash], true
}

// List returns a sorted slice of all the IPs in the hash ring.
//
// This method is safe for concurrent use by multiple goroutines.
func (h *HashRing) List() []string {
	rt := h.mu.RLock()
	defer h.mu.RUnlock(rt)

	ips := make([]string, 0, len(h.ips))
	for _, ip := range h.ips {
		ips = append(ips, ip)
	}

	slices.Sort(ips)

	return ips
}
