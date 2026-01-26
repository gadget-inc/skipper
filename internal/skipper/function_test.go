package skipper

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
	"google.golang.org/protobuf/proto"
	"gotest.tools/v3/assert"
)

var testFunction = Function_builder{
	Namespace:  proto.String("skipper-production"),
	Deployment: proto.String("my-awesome-app-deployment"),
	Tenant:     proto.String("tenant-12345-abcdef"),
	Metadata:   proto.String("some-metadata-value"),
	Scale: Scale_builder{
		MinInstances:           proto.Uint32(1),
		MaxInstances:           proto.Uint32(10),
		TargetCpuUsageMilli:    proto.Uint32(500),
		TargetMemoryUsageMib:   proto.Uint32(256),
		TargetInFlightRequests: proto.Uint32(100),
	}.Build(),
}.Build()

var testFunctions = []*Function{
	Function_builder{Namespace: proto.String("ns1"), Deployment: proto.String("deploy1"), Tenant: proto.String("tenant1"), Metadata: proto.String("meta1"), Scale: Scale_builder{MinInstances: proto.Uint32(1), MaxInstances: proto.Uint32(10), TargetCpuUsageMilli: proto.Uint32(500), TargetMemoryUsageMib: proto.Uint32(256), TargetInFlightRequests: proto.Uint32(100)}.Build()}.Build(),
	Function_builder{Namespace: proto.String("ns2"), Deployment: proto.String("deploy2"), Tenant: proto.String("tenant2"), Metadata: proto.String("meta2"), Scale: Scale_builder{MinInstances: proto.Uint32(2), MaxInstances: proto.Uint32(20), TargetCpuUsageMilli: proto.Uint32(600), TargetMemoryUsageMib: proto.Uint32(512), TargetInFlightRequests: proto.Uint32(200)}.Build()}.Build(),
	Function_builder{Namespace: proto.String("ns3"), Deployment: proto.String("deploy3"), Tenant: proto.String("tenant3"), Metadata: proto.String("meta3"), Scale: Scale_builder{MinInstances: proto.Uint32(3), MaxInstances: proto.Uint32(30), TargetCpuUsageMilli: proto.Uint32(700), TargetMemoryUsageMib: proto.Uint32(1024), TargetInFlightRequests: proto.Uint32(300)}.Build()}.Build(),
	Function_builder{Namespace: proto.String("ns4"), Deployment: proto.String("deploy4"), Tenant: proto.String("tenant4"), Metadata: proto.String("meta4"), Scale: Scale_builder{MinInstances: proto.Uint32(4), MaxInstances: proto.Uint32(40), TargetCpuUsageMilli: proto.Uint32(800), TargetMemoryUsageMib: proto.Uint32(2048), TargetInFlightRequests: proto.Uint32(400)}.Build()}.Build(),
	Function_builder{Namespace: proto.String("ns5"), Deployment: proto.String("deploy5"), Tenant: proto.String("tenant5"), Metadata: proto.String("meta5"), Scale: Scale_builder{MinInstances: proto.Uint32(5), MaxInstances: proto.Uint32(50), TargetCpuUsageMilli: proto.Uint32(900), TargetMemoryUsageMib: proto.Uint32(4096), TargetInFlightRequests: proto.Uint32(500)}.Build()}.Build(),
}

var benchHashSeed = maphash.MakeSeed()

func TestHashNoCollisions(t *testing.T) {
	// Test that different Functions with concatenated strings that would match
	// without separators produce different hashes
	f1 := Function_builder{Namespace: proto.String("ab"), Deployment: proto.String("cd"), Tenant: proto.String(""), Metadata: proto.String(""), Scale: Scale_builder{}.Build()}.Build()
	f2 := Function_builder{Namespace: proto.String("abc"), Deployment: proto.String("d"), Tenant: proto.String(""), Metadata: proto.String(""), Scale: Scale_builder{}.Build()}.Build()
	f3 := Function_builder{Namespace: proto.String("a"), Deployment: proto.String("bcd"), Tenant: proto.String(""), Metadata: proto.String(""), Scale: Scale_builder{}.Build()}.Build()
	f4 := Function_builder{Namespace: proto.String("abcd"), Deployment: proto.String(""), Tenant: proto.String(""), Metadata: proto.String(""), Scale: Scale_builder{}.Build()}.Build()

	assert.Assert(t, f1.Hash() != f2.Hash(), "f1 and f2 should have different hashes")
	assert.Assert(t, f1.Hash() != f3.Hash(), "f1 and f3 should have different hashes")
	assert.Assert(t, f1.Hash() != f4.Hash(), "f1 and f4 should have different hashes")
	assert.Assert(t, f2.Hash() != f3.Hash(), "f2 and f3 should have different hashes")
	assert.Assert(t, f2.Hash() != f4.Hash(), "f2 and f4 should have different hashes")
	assert.Assert(t, f3.Hash() != f4.Hash(), "f3 and f4 should have different hashes")

	// Also test across Tenant and Metadata fields
	f5 := Function_builder{Namespace: proto.String("ns"), Deployment: proto.String("dep"), Tenant: proto.String("ab"), Metadata: proto.String("cd"), Scale: Scale_builder{}.Build()}.Build()
	f6 := Function_builder{Namespace: proto.String("ns"), Deployment: proto.String("dep"), Tenant: proto.String("abc"), Metadata: proto.String("d"), Scale: Scale_builder{}.Build()}.Build()
	assert.Assert(t, f5.Hash() != f6.Hash(), "f5 and f6 should have different hashes")

	// Identical functions should have identical hashes
	f7 := Function_builder{Namespace: proto.String("ns"), Deployment: proto.String("dep"), Tenant: proto.String("tenant"), Metadata: proto.String("meta"), Scale: Scale_builder{}.Build()}.Build()
	f8 := Function_builder{Namespace: proto.String("ns"), Deployment: proto.String("dep"), Tenant: proto.String("tenant"), Metadata: proto.String("meta"), Scale: Scale_builder{}.Build()}.Build()
	assert.Assert(t, f7.Hash() == f8.Hash(), "identical functions should have the same hash")
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
		m := make(map[FunctionHash]int)
		for i, fn := range testFunctions {
			m[fn.Hash()] = i
		}

		lookupHash := testFunctions[2].Hash()

		for b.Loop() {
			_ = m[lookupHash]
		}
	})

	b.Run("HashKeyWithCompute", func(b *testing.B) {
		m := make(map[FunctionHash]int)
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
		f.GetNamespace(),
		f.GetDeployment(),
		f.GetTenant(),
		f.GetMetadata(),
		f.GetScale().GetMinInstances(),
		f.GetScale().GetMaxInstances(),
		f.GetScale().GetTargetCpuUsageMilli(),
		f.GetScale().GetTargetMemoryUsageMib(),
		f.GetScale().GetTargetInFlightRequests(),
	)
}

// hashStrconv uses strconv.AppendInt with a pre-allocated buffer
func hashStrconv(f *Function) string {
	buf := make([]byte, 0, len(f.GetNamespace())+len(f.GetDeployment())+len(f.GetTenant())+len(f.GetMetadata())+64)
	buf = append(buf, f.GetNamespace()...)
	buf = append(buf, '/')
	buf = append(buf, f.GetDeployment()...)
	buf = append(buf, '/')
	buf = append(buf, f.GetTenant()...)
	buf = append(buf, '/')
	buf = append(buf, f.GetMetadata()...)
	buf = append(buf, '/')
	buf = strconv.AppendInt(buf, int64(f.GetScale().GetMinInstances()), 10)
	buf = append(buf, '/')
	buf = strconv.AppendInt(buf, int64(f.GetScale().GetMaxInstances()), 10)
	buf = append(buf, '/')
	buf = strconv.AppendInt(buf, int64(f.GetScale().GetTargetCpuUsageMilli()), 10)
	buf = append(buf, '/')
	buf = strconv.AppendInt(buf, int64(f.GetScale().GetTargetMemoryUsageMib()), 10)
	buf = append(buf, '/')
	buf = strconv.AppendInt(buf, int64(f.GetScale().GetTargetInFlightRequests()), 10)
	return string(buf)
}

// hashStringsBuilder uses strings.Builder
func hashStringsBuilder(f *Function) string {
	var b strings.Builder
	b.Grow(len(f.GetNamespace()) + len(f.GetDeployment()) + len(f.GetTenant()) + len(f.GetMetadata()) + 64)
	b.WriteString(f.GetNamespace())
	b.WriteByte('/')
	b.WriteString(f.GetDeployment())
	b.WriteByte('/')
	b.WriteString(f.GetTenant())
	b.WriteByte('/')
	b.WriteString(f.GetMetadata())
	b.WriteByte('/')
	b.WriteString(strconv.FormatUint(uint64(f.GetScale().GetMinInstances()), 10))
	b.WriteByte('/')
	b.WriteString(strconv.FormatUint(uint64(f.GetScale().GetMaxInstances()), 10))
	b.WriteByte('/')
	b.WriteString(strconv.FormatUint(uint64(f.GetScale().GetTargetCpuUsageMilli()), 10))
	b.WriteByte('/')
	b.WriteString(strconv.FormatUint(uint64(f.GetScale().GetTargetMemoryUsageMib()), 10))
	b.WriteByte('/')
	b.WriteString(strconv.FormatUint(uint64(f.GetScale().GetTargetInFlightRequests()), 10))
	return b.String()
}

// hashFNV64 uses FNV-64a hash
func hashFNV64(f *Function) uint64 {
	h := fnv.New64a()
	h.Write([]byte(f.GetNamespace()))
	h.Write([]byte{0})
	h.Write([]byte(f.GetDeployment()))
	h.Write([]byte{0})
	h.Write([]byte(f.GetTenant()))
	h.Write([]byte{0})
	h.Write([]byte(f.GetMetadata()))
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], f.GetScale().GetMinInstances())
	h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:], f.GetScale().GetMaxInstances())
	h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:], f.GetScale().GetTargetCpuUsageMilli())
	h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:], f.GetScale().GetTargetMemoryUsageMib())
	h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:], f.GetScale().GetTargetInFlightRequests())
	h.Write(buf[:])
	return h.Sum64()
}

// hashXXHash uses xxhash
func hashXXHash(f *Function) uint64 {
	h := xxhash.New()
	_, _ = h.WriteString(f.GetNamespace())
	_, _ = h.Write([]byte{0})
	_, _ = h.WriteString(f.GetDeployment())
	_, _ = h.Write([]byte{0})
	_, _ = h.WriteString(f.GetTenant())
	_, _ = h.Write([]byte{0})
	_, _ = h.WriteString(f.GetMetadata())
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], f.GetScale().GetMinInstances())
	_, _ = h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:], f.GetScale().GetMaxInstances())
	_, _ = h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:], f.GetScale().GetTargetCpuUsageMilli())
	_, _ = h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:], f.GetScale().GetTargetMemoryUsageMib())
	_, _ = h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:], f.GetScale().GetTargetInFlightRequests())
	_, _ = h.Write(buf[:])
	return h.Sum64()
}

// hashMapHash uses Go's built-in maphash
func hashMapHash(f *Function) uint64 {
	var h maphash.Hash
	h.SetSeed(benchHashSeed)
	h.WriteString(f.GetNamespace())
	h.WriteByte(0)
	h.WriteString(f.GetDeployment())
	h.WriteByte(0)
	h.WriteString(f.GetTenant())
	h.WriteByte(0)
	h.WriteString(f.GetMetadata())
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], f.GetScale().GetMinInstances())
	h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:], f.GetScale().GetMaxInstances())
	h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:], f.GetScale().GetTargetCpuUsageMilli())
	h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:], f.GetScale().GetTargetMemoryUsageMib())
	h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:], f.GetScale().GetTargetInFlightRequests())
	h.Write(buf[:])
	return h.Sum64()
}

func TestFunctionFromHeader(t *testing.T) {
	validFn := Function_builder{
		Namespace:  proto.String("test-ns"),
		Deployment: proto.String("test-deploy"),
		Tenant:     proto.String("test-tenant"),
		Metadata:   proto.String("test-metadata"),
		Scale: Scale_builder{
			MinInstances:           proto.Uint32(1),
			MaxInstances:           proto.Uint32(10),
			TargetCpuUsageMilli:    proto.Uint32(500),
			TargetMemoryUsageMib:   proto.Uint32(256),
			TargetInFlightRequests: proto.Uint32(100),
		}.Build(),
	}.Build()

	tests := []struct {
		name    string
		header  string
		wantErr string
		wantFn  *Function
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
			wantErr: "invalid value for uint32 field",
		},
		{
			name:    "negative max instances",
			header:  `{"namespace":"n","deployment":"d","tenant":"t","scale":{"max_instances":-1}}`,
			wantErr: "invalid value for uint32 field",
		},
		{
			name:    "negative target cpu usage",
			header:  `{"namespace":"n","deployment":"d","tenant":"t","scale":{"target_cpu_usage_milli":-1}}`,
			wantErr: "invalid value for uint32 field",
		},
		{
			name:    "negative target memory usage",
			header:  `{"namespace":"n","deployment":"d","tenant":"t","scale":{"target_memory_usage_mib":-1}}`,
			wantErr: "invalid value for uint32 field",
		},
		{
			name:    "negative target in flight requests",
			header:  `{"namespace":"n","deployment":"d","tenant":"t","scale":{"target_in_flight_requests":-1}}`,
			wantErr: "invalid value for uint32 field",
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

			fn, err := FunctionFromHeader(req)

			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				assert.Assert(t, fn == nil, "expected nil function on error")
			} else {
				assert.NilError(t, err)
				assert.Assert(t, proto.Equal(fn, tc.wantFn), "function mismatch: got %+v, want %+v", fn, tc.wantFn)
			}
		})
	}
}
