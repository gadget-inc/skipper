package hashring

import (
	"hash/crc32"
	"sort"
	"sync"
)

// this entire file is curtesy of gpt-01-preview :)

// Node represents a node in the hash ring.
type Node struct {
	IP string // Unique identifier for the node, e.g., IP address or hostname
}

// HashRing represents a thread-safe consistent hash ring without replicas.
type HashRing struct {
	nodes  map[uint32]Node // Map from hash to node
	hashes []uint32        // Sorted list of hashes
	mu     sync.RWMutex    // Read-Write mutex to protect concurrent access
}

// New creates a new HashRing.
//
// Example:
//
//	ring := New()
func New() *HashRing {
	return &HashRing{
		nodes:  make(map[uint32]Node),
		hashes: []uint32{},
	}
}

// AddNode adds a node to the hash ring.
//
// node: The node to be added.
//
// This method is safe for concurrent use by multiple goroutines.
//
// Example:
//
//	ring.AddNode(Node{Name: "127.0.0.1:5000"})
func (h *HashRing) AddNode(node Node) {
	// Compute the hash of the node's name
	hash := crc32.ChecksumIEEE([]byte(node.IP))

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if the node already exists
	if _, exists := h.nodes[hash]; exists {
		return
	}

	// Map the hash to the node
	h.nodes[hash] = node
	// Append the hash to the list of hashes
	h.hashes = append(h.hashes, hash)
	// Sort the hashes to maintain the ring order
	sort.Slice(h.hashes, func(i, j int) bool { return h.hashes[i] < h.hashes[j] })
}

// RemoveNode removes a node from the hash ring.
//
// node: The node to be removed.
//
// This method is safe for concurrent use by multiple goroutines.
//
// Example:
//
//	ring.RemoveNode(Node{Name: "127.0.0.1:5000"})
func (h *HashRing) RemoveNode(node Node) {
	// Compute the hash of the node's name
	hash := crc32.ChecksumIEEE([]byte(node.IP))

	h.mu.Lock()
	defer h.mu.Unlock()

	// Remove the node from the map
	delete(h.nodes, hash)
	// Remove hash from the sorted list of hashes
	index := sort.Search(len(h.hashes), func(i int) bool { return h.hashes[i] >= hash })
	if index < len(h.hashes) && h.hashes[index] == hash {
		h.hashes = append(h.hashes[:index], h.hashes[index+1:]...)
	}
}

// GetNode returns the node responsible for the given key.
//
// key: The key for which to find the responsible node.
//
// This method is safe for concurrent use by multiple goroutines.
//
// Example:
//
//	node := ring.GetNode("my-cache-key")
func (h *HashRing) GetNode(key string) (Node, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.hashes) == 0 {
		return Node{}, false
	}
	// Compute the hash of the key
	hash := crc32.ChecksumIEEE([]byte(key))
	// Locate the nearest node greater than or equal to the key's hash
	index := sort.Search(len(h.hashes), func(i int) bool { return h.hashes[i] >= hash })
	if index == len(h.hashes) {
		// Wrap around to the first node
		index = 0
	}
	nodeHash := h.hashes[index]
	return h.nodes[nodeHash], true
}

// ListNodes returns a slice of all nodes in the hash ring.
//
// This method is safe for concurrent use by multiple goroutines.
func (h *HashRing) ListNodes() []Node {
	h.mu.RLock()
	defer h.mu.RUnlock()

	nodes := make([]Node, 0, len(h.nodes))
	for _, node := range h.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}
