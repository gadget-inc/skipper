package function

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"hash/maphash"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/cespare/xxhash/v2"
	"github.com/gadget-inc/skipper/internal/key"
	"gotest.tools/v3/assert"
)

func TestFromHeader(t *testing.T) {
	validFn := &Function{
		Namespace:  "test-ns",
		Deployment: "test-deploy",
		Tenant:     "test-tenant",
		Metadata:   "test-metadata",
		Scale: &Scale{
			MinInstances:           1,
			MaxInstances:           10,
			TargetCPUUsageMilli:    500,
			TargetMemoryUsageMiB:   256,
			TargetInFlightRequests: 100,
		},
	}

	tests := []struct {
		name        string
		setupHeader func(req *httptest.ResponseRecorder) *httptest.ResponseRecorder
		header      string
		wantErr     string
		wantFn      *Function
	}{
		{
			name:    "missing header",
			header:  "",
			wantErr: "missing " + key.Function.Header,
		},
		{
			name:    "invalid JSON",
			header:  "{invalid json}",
			wantErr: "failed to unmarshal " + key.Function.Header + " header:",
		},
		{
			name:    "missing namespace",
			header:  `{"deployment":"d","tenant":"t"}`,
			wantErr: "missing namespace",
		},
		{
			name:    "missing deployment",
			header:  `{"namespace":"n","tenant":"t"}`,
			wantErr: "missing deployment",
		},
		{
			name:    "missing tenant",
			header:  `{"namespace":"n","deployment":"d"}`,
			wantErr: "missing tenant",
		},
		{
			name:    "negative min instances",
			header:  `{"namespace":"n","deployment":"d","tenant":"t","scale":{"min_instances":-1}}`,
			wantErr: "min instances must be greater than or equal to 0",
		},
		{
			name:    "negative max instances",
			header:  `{"namespace":"n","deployment":"d","tenant":"t","scale":{"max_instances":-1}}`,
			wantErr: "max instances must be greater than or equal to 0",
		},
		{
			name:    "negative target cpu usage",
			header:  `{"namespace":"n","deployment":"d","tenant":"t","scale":{"target_cpu_usage_milli":-1}}`,
			wantErr: "target cpu usage must be greater than or equal to 0",
		},
		{
			name:    "negative target memory usage",
			header:  `{"namespace":"n","deployment":"d","tenant":"t","scale":{"target_memory_usage_mib":-1}}`,
			wantErr: "target memory usage must be greater than or equal to 0",
		},
		{
			name:    "negative target in flight requests",
			header:  `{"namespace":"n","deployment":"d","tenant":"t","scale":{"target_in_flight_requests":-1}}`,
			wantErr: "target in flight requests must be greater than or equal to 0",
		},
		{
			name:    "nil scale",
			header:  `{"namespace":"n","deployment":"d","tenant":"t"}`,
			wantErr: "missing scale",
		},
		{
			name:   "valid function with scale",
			header: `{"namespace":"test-ns","deployment":"test-deploy","tenant":"test-tenant","metadata":"test-metadata","scale":{"min_instances":1,"max_instances":10,"target_cpu_usage_milli":500,"target_memory_usage_mib":256,"target_in_flight_requests":100}}`,
			wantFn: validFn,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tc.header != "" {
				req.Header.Set(key.Function.Header, tc.header)
			}

			fn, err := FromHeader(req)

			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				assert.Assert(t, fn == nil, "expected nil function on error")
			} else {
				assert.NilError(t, err)
				assert.Assert(t, fn.Equal(tc.wantFn), "function mismatch: got %+v, want %+v", fn, tc.wantFn)
			}
		})
	}
}

func TestHashNoCollisions(t *testing.T) {
	// Test that different Functions with concatenated strings that would match
	// without separators produce different hashes
	f1 := &Function{Namespace: "ab", Deployment: "cd", Tenant: "", Metadata: "", Scale: &Scale{}}
	f2 := &Function{Namespace: "abc", Deployment: "d", Tenant: "", Metadata: "", Scale: &Scale{}}
	f3 := &Function{Namespace: "a", Deployment: "bcd", Tenant: "", Metadata: "", Scale: &Scale{}}
	f4 := &Function{Namespace: "abcd", Deployment: "", Tenant: "", Metadata: "", Scale: &Scale{}}

	assert.Assert(t, f1.Hash() != f2.Hash(), "f1 and f2 should have different hashes")
	assert.Assert(t, f1.Hash() != f3.Hash(), "f1 and f3 should have different hashes")
	assert.Assert(t, f1.Hash() != f4.Hash(), "f1 and f4 should have different hashes")
	assert.Assert(t, f2.Hash() != f3.Hash(), "f2 and f3 should have different hashes")
	assert.Assert(t, f2.Hash() != f4.Hash(), "f2 and f4 should have different hashes")
	assert.Assert(t, f3.Hash() != f4.Hash(), "f3 and f4 should have different hashes")

	// Also test across Tenant and Metadata fields
	f5 := &Function{Namespace: "ns", Deployment: "dep", Tenant: "ab", Metadata: "cd", Scale: &Scale{}}
	f6 := &Function{Namespace: "ns", Deployment: "dep", Tenant: "abc", Metadata: "d", Scale: &Scale{}}
	assert.Assert(t, f5.Hash() != f6.Hash(), "f5 and f6 should have different hashes")

	// Identical functions should have identical hashes
	f7 := &Function{Namespace: "ns", Deployment: "dep", Tenant: "tenant", Metadata: "meta", Scale: &Scale{}}
	f8 := &Function{Namespace: "ns", Deployment: "dep", Tenant: "tenant", Metadata: "meta", Scale: &Scale{}}
	assert.Assert(t, f7.Hash() == f8.Hash(), "identical functions should have the same hash")
}

var testFunction = &Function{
	Namespace:  "skipper-production",
	Deployment: "my-awesome-app-deployment",
	Tenant:     "tenant-12345-abcdef",
	Metadata:   "some-metadata-value",
	Scale: &Scale{
		MinInstances:           1,
		MaxInstances:           10,
		TargetCPUUsageMilli:    500,
		TargetMemoryUsageMiB:   256,
		TargetInFlightRequests: 100,
	},
}

var testFunctions = []*Function{
	{Namespace: "ns1", Deployment: "deploy1", Tenant: "tenant1", Metadata: "meta1", Scale: &Scale{MinInstances: 1, MaxInstances: 10, TargetCPUUsageMilli: 500, TargetMemoryUsageMiB: 256, TargetInFlightRequests: 100}},
	{Namespace: "ns2", Deployment: "deploy2", Tenant: "tenant2", Metadata: "meta2", Scale: &Scale{MinInstances: 2, MaxInstances: 20, TargetCPUUsageMilli: 600, TargetMemoryUsageMiB: 512, TargetInFlightRequests: 200}},
	{Namespace: "ns3", Deployment: "deploy3", Tenant: "tenant3", Metadata: "meta3", Scale: &Scale{MinInstances: 3, MaxInstances: 30, TargetCPUUsageMilli: 700, TargetMemoryUsageMiB: 1024, TargetInFlightRequests: 300}},
	{Namespace: "ns4", Deployment: "deploy4", Tenant: "tenant4", Metadata: "meta4", Scale: &Scale{MinInstances: 4, MaxInstances: 40, TargetCPUUsageMilli: 800, TargetMemoryUsageMiB: 2048, TargetInFlightRequests: 400}},
	{Namespace: "ns5", Deployment: "deploy5", Tenant: "tenant5", Metadata: "meta5", Scale: &Scale{MinInstances: 5, MaxInstances: 50, TargetCPUUsageMilli: 900, TargetMemoryUsageMiB: 4096, TargetInFlightRequests: 500}},
}

func BenchmarkHash(b *testing.B) {
	b.Run("Sprintf", func(b *testing.B) {
		for b.Loop() {
			_ = hashSprintf(testFunction)
		}
	})

	b.Run("Strconv", func(b *testing.B) {
		for b.Loop() {
			_ = hashStrconv(testFunction)
		}
	})

	b.Run("StringsBuilder", func(b *testing.B) {
		for b.Loop() {
			_ = hashStringsBuilder(testFunction)
		}
	})

	b.Run("FNV64", func(b *testing.B) {
		for b.Loop() {
			_ = hashFNV64(testFunction)
		}
	})

	b.Run("XXHash", func(b *testing.B) {
		for b.Loop() {
			_ = hashXXHash(testFunction)
		}
	})

	b.Run("MapHash", func(b *testing.B) {
		for b.Loop() {
			_ = hashMapHash(testFunction)
		}
	})
}

func BenchmarkMapLookup(b *testing.B) {
	b.Run("HashKey", func(b *testing.B) {
		m := make(map[Hash]int)
		for i, fn := range testFunctions {
			m[fn.Hash()] = i
		}

		lookupHash := testFunctions[2].Hash()

		for b.Loop() {
			_ = m[lookupHash]
		}
	})

	b.Run("HashKeyWithCompute", func(b *testing.B) {
		m := make(map[Hash]int)
		for i, fn := range testFunctions {
			m[fn.Hash()] = i
		}

		lookupFn := testFunctions[2]

		for b.Loop() {
			_ = m[lookupFn.Hash()]
		}
	})
}

// hashSprintf uses fmt.Sprintf to build a string hash
func hashSprintf(f *Function) string {
	return fmt.Sprintf("%s/%s/%s/%s/%d/%d/%d/%d/%d",
		f.Namespace,
		f.Deployment,
		f.Tenant,
		f.Metadata,
		f.Scale.MinInstances,
		f.Scale.MaxInstances,
		f.Scale.TargetCPUUsageMilli,
		f.Scale.TargetMemoryUsageMiB,
		f.Scale.TargetInFlightRequests,
	)
}

// hashStrconv uses strconv.AppendInt with a pre-allocated buffer
func hashStrconv(f *Function) string {
	buf := make([]byte, 0, len(f.Namespace)+len(f.Deployment)+len(f.Tenant)+len(f.Metadata)+64)
	buf = append(buf, f.Namespace...)
	buf = append(buf, '/')
	buf = append(buf, f.Deployment...)
	buf = append(buf, '/')
	buf = append(buf, f.Tenant...)
	buf = append(buf, '/')
	buf = append(buf, f.Metadata...)
	buf = append(buf, '/')
	buf = strconv.AppendInt(buf, int64(f.Scale.MinInstances), 10)
	buf = append(buf, '/')
	buf = strconv.AppendInt(buf, int64(f.Scale.MaxInstances), 10)
	buf = append(buf, '/')
	buf = strconv.AppendInt(buf, int64(f.Scale.TargetCPUUsageMilli), 10)
	buf = append(buf, '/')
	buf = strconv.AppendInt(buf, int64(f.Scale.TargetMemoryUsageMiB), 10)
	buf = append(buf, '/')
	buf = strconv.AppendInt(buf, int64(f.Scale.TargetInFlightRequests), 10)
	return string(buf)
}

// hashStringsBuilder uses strings.Builder
func hashStringsBuilder(f *Function) string {
	var b strings.Builder
	b.Grow(len(f.Namespace) + len(f.Deployment) + len(f.Tenant) + len(f.Metadata) + 64)
	b.WriteString(f.Namespace)
	b.WriteByte('/')
	b.WriteString(f.Deployment)
	b.WriteByte('/')
	b.WriteString(f.Tenant)
	b.WriteByte('/')
	b.WriteString(f.Metadata)
	b.WriteByte('/')
	b.WriteString(strconv.Itoa(f.Scale.MinInstances))
	b.WriteByte('/')
	b.WriteString(strconv.Itoa(f.Scale.MaxInstances))
	b.WriteByte('/')
	b.WriteString(strconv.Itoa(f.Scale.TargetCPUUsageMilli))
	b.WriteByte('/')
	b.WriteString(strconv.Itoa(f.Scale.TargetMemoryUsageMiB))
	b.WriteByte('/')
	b.WriteString(strconv.Itoa(f.Scale.TargetInFlightRequests))
	return b.String()
}

// hashFNV64 uses FNV-64a hash
func hashFNV64(f *Function) uint64 {
	h := fnv.New64a()
	h.Write([]byte(f.Namespace))
	h.Write([]byte{0})
	h.Write([]byte(f.Deployment))
	h.Write([]byte{0})
	h.Write([]byte(f.Tenant))
	h.Write([]byte{0})
	h.Write([]byte(f.Metadata))
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(f.Scale.MinInstances))
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], uint64(f.Scale.MaxInstances))
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], uint64(f.Scale.TargetCPUUsageMilli))
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], uint64(f.Scale.TargetMemoryUsageMiB))
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], uint64(f.Scale.TargetInFlightRequests))
	h.Write(buf[:])
	return h.Sum64()
}

// hashXXHash uses xxhash
func hashXXHash(f *Function) uint64 {
	h := xxhash.New()
	_, _ = h.WriteString(f.Namespace)
	_, _ = h.Write([]byte{0})
	_, _ = h.WriteString(f.Deployment)
	_, _ = h.Write([]byte{0})
	_, _ = h.WriteString(f.Tenant)
	_, _ = h.Write([]byte{0})
	_, _ = h.WriteString(f.Metadata)
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(f.Scale.MinInstances))
	_, _ = h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], uint64(f.Scale.MaxInstances))
	_, _ = h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], uint64(f.Scale.TargetCPUUsageMilli))
	_, _ = h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], uint64(f.Scale.TargetMemoryUsageMiB))
	_, _ = h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], uint64(f.Scale.TargetInFlightRequests))
	_, _ = h.Write(buf[:])
	return h.Sum64()
}

var benchHashSeed = maphash.MakeSeed()

// hashMapHash uses Go's built-in maphash
func hashMapHash(f *Function) uint64 {
	var h maphash.Hash
	h.SetSeed(benchHashSeed)
	h.WriteString(f.Namespace)
	h.WriteByte(0)
	h.WriteString(f.Deployment)
	h.WriteByte(0)
	h.WriteString(f.Tenant)
	h.WriteByte(0)
	h.WriteString(f.Metadata)
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(f.Scale.MinInstances))
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], uint64(f.Scale.MaxInstances))
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], uint64(f.Scale.TargetCPUUsageMilli))
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], uint64(f.Scale.TargetMemoryUsageMiB))
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], uint64(f.Scale.TargetInFlightRequests))
	h.Write(buf[:])
	return h.Sum64()
}
