package resolver

import (
	"sort"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/resolver"
	"gotest.tools/v3/assert"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	ktest "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"
)

// mockClientConn captures resolver state updates for test assertions.
type mockClientConn struct {
	resolver.ClientConn
	mu     sync.Mutex
	states []resolver.State
	errors []error
	ch     chan struct{} // signals each UpdateState call
}

func newMockCC() *mockClientConn {
	return &mockClientConn{ch: make(chan struct{}, 16)}
}

func (m *mockClientConn) UpdateState(s resolver.State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states = append(m.states, s)
	m.ch <- struct{}{}
	return nil
}

func (m *mockClientConn) ReportError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors = append(m.errors, err)
}

func (m *mockClientConn) lastState() resolver.State {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.states) == 0 {
		return resolver.State{}
	}
	return m.states[len(m.states)-1]
}

func (m *mockClientConn) waitForUpdate(t *testing.T) {
	t.Helper()
	select {
	case <-m.ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for resolver state update")
	}
}

// waitForAddrs waits until the last resolver state contains exactly n endpoints,
// draining intermediate updates (e.g. from informer cache sync).
func (m *mockClientConn) waitForAddrs(t *testing.T, n int) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		m.mu.Lock()
		count := 0
		if len(m.states) > 0 {
			count = len(m.states[len(m.states)-1].Endpoints)
		}
		m.mu.Unlock()
		if count == n {
			return
		}
		select {
		case <-m.ch:
		case <-deadline:
			t.Fatalf("timed out waiting for %d endpoints, last state has %d", n, count)
		}
	}
}

func sortedAddrs(s resolver.State) []string {
	out := make([]string, 0, len(s.Endpoints))
	for _, ep := range s.Endpoints {
		for _, a := range ep.Addresses {
			out = append(out, a.Addr)
		}
	}
	sort.Strings(out)
	return out
}

func makeEndpointSlice(name, namespace, serviceName string, endpoints []discoveryv1.Endpoint) *discoveryv1.EndpointSlice {
	return &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: serviceName,
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints:   endpoints,
	}
}

func readyEndpoint(addr, zone string) discoveryv1.Endpoint {
	ep := discoveryv1.Endpoint{
		Addresses:  []string{addr},
		Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)},
	}
	if zone != "" {
		ep.Zone = &zone
	}
	return ep
}

func notReadyEndpoint(addr, zone string) discoveryv1.Endpoint {
	ep := discoveryv1.Endpoint{
		Addresses:  []string{addr},
		Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(false)},
	}
	if zone != "" {
		ep.Zone = &zone
	}
	return ep
}

func TestSameZoneEndpointsPreferred(t *testing.T) {
	client := fake.NewSimpleClientset( //nolint:staticcheck // NewClientset isn't generated for discovery/v1
		makeEndpointSlice("svc-abc", "ns", "controller-headless", []discoveryv1.Endpoint{
			readyEndpoint("10.0.1.1", "us-east-1a"),
			readyEndpoint("10.0.2.1", "us-east-1b"),
			readyEndpoint("10.0.1.2", "us-east-1a"),
		}),
	)

	cc := newMockCC()
	b := New(client, "ns", "controller-headless", 50051, "us-east-1a")

	r, err := b.Build(resolver.Target{}, cc, resolver.BuildOptions{})
	assert.NilError(t, err)
	defer r.Close()

	cc.waitForUpdate(t)

	got := sortedAddrs(cc.lastState())
	assert.DeepEqual(t, got, []string{"10.0.1.1:50051", "10.0.1.2:50051"})
}

func TestFallbackToAllZonesWhenNoSameZone(t *testing.T) {
	client := fake.NewSimpleClientset( //nolint:staticcheck // NewClientset isn't generated for discovery/v1
		makeEndpointSlice("svc-abc", "ns", "controller-headless", []discoveryv1.Endpoint{
			readyEndpoint("10.0.2.1", "us-east-1b"),
			readyEndpoint("10.0.3.1", "us-east-1c"),
		}),
	)

	cc := newMockCC()
	b := New(client, "ns", "controller-headless", 50051, "us-east-1a")

	r, err := b.Build(resolver.Target{}, cc, resolver.BuildOptions{})
	assert.NilError(t, err)
	defer r.Close()

	cc.waitForUpdate(t)

	got := sortedAddrs(cc.lastState())
	assert.DeepEqual(t, got, []string{"10.0.2.1:50051", "10.0.3.1:50051"})
}

func TestNotReadyEndpointsFiltered(t *testing.T) {
	client := fake.NewSimpleClientset( //nolint:staticcheck // NewClientset isn't generated for discovery/v1
		makeEndpointSlice("svc-abc", "ns", "controller-headless", []discoveryv1.Endpoint{
			readyEndpoint("10.0.1.1", "us-east-1a"),
			notReadyEndpoint("10.0.1.2", "us-east-1a"),
			readyEndpoint("10.0.2.1", "us-east-1b"),
		}),
	)

	cc := newMockCC()
	b := New(client, "ns", "controller-headless", 50051, "us-east-1a")

	r, err := b.Build(resolver.Target{}, cc, resolver.BuildOptions{})
	assert.NilError(t, err)
	defer r.Close()

	cc.waitForUpdate(t)

	got := sortedAddrs(cc.lastState())
	assert.DeepEqual(t, got, []string{"10.0.1.1:50051"})
}

func TestEmptyZoneUsesAllReadyEndpoints(t *testing.T) {
	client := fake.NewSimpleClientset( //nolint:staticcheck // NewClientset isn't generated for discovery/v1
		makeEndpointSlice("svc-abc", "ns", "controller-headless", []discoveryv1.Endpoint{
			readyEndpoint("10.0.1.1", "us-east-1a"),
			readyEndpoint("10.0.2.1", "us-east-1b"),
			notReadyEndpoint("10.0.3.1", "us-east-1c"),
		}),
	)

	cc := newMockCC()
	b := New(client, "ns", "controller-headless", 50051, "")

	r, err := b.Build(resolver.Target{}, cc, resolver.BuildOptions{})
	assert.NilError(t, err)
	defer r.Close()

	cc.waitForUpdate(t)

	got := sortedAddrs(cc.lastState())
	assert.DeepEqual(t, got, []string{"10.0.1.1:50051", "10.0.2.1:50051"})
}

func TestEndpointSliceUpdateTriggersReResolution(t *testing.T) {
	client := fake.NewSimpleClientset( //nolint:staticcheck // NewClientset isn't generated for discovery/v1
		makeEndpointSlice("svc-abc", "ns", "controller-headless", []discoveryv1.Endpoint{
			readyEndpoint("10.0.1.1", "us-east-1a"),
		}),
	)

	// Signal when the informer's Watch is established so we don't create
	// a new EndpointSlice in the gap between the informer's initial List
	// and Watch (the fake client doesn't replay missed events).
	watchReady := make(chan struct{}, 1)
	client.PrependWatchReactor("endpointslices", func(_ ktest.Action) (bool, watch.Interface, error) {
		select {
		case watchReady <- struct{}{}:
		default:
		}
		return false, nil, nil
	})

	cc := newMockCC()
	b := New(client, "ns", "controller-headless", 50051, "us-east-1a")

	r, err := b.Build(resolver.Target{}, cc, resolver.BuildOptions{})
	assert.NilError(t, err)
	defer r.Close()

	cc.waitForUpdate(t)

	got := sortedAddrs(cc.lastState())
	assert.DeepEqual(t, got, []string{"10.0.1.1:50051"})

	// Wait for the informer's watch to be established before creating
	// the new EndpointSlice.
	select {
	case <-watchReady:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for watch to be established")
	}

	// Add a new endpoint via a second EndpointSlice
	_, err = client.DiscoveryV1().EndpointSlices("ns").Create(t.Context(),
		makeEndpointSlice("svc-def", "ns", "controller-headless", []discoveryv1.Endpoint{
			readyEndpoint("10.0.1.3", "us-east-1a"),
		}),
		metav1.CreateOptions{},
	)
	assert.NilError(t, err)

	cc.waitForAddrs(t, 2)

	got = sortedAddrs(cc.lastState())
	assert.DeepEqual(t, got, []string{"10.0.1.1:50051", "10.0.1.3:50051"})
}

func TestMultipleEndpointSlices(t *testing.T) {
	client := fake.NewSimpleClientset( //nolint:staticcheck // NewClientset isn't generated for discovery/v1
		makeEndpointSlice("svc-abc", "ns", "controller-headless", []discoveryv1.Endpoint{
			readyEndpoint("10.0.1.1", "us-east-1a"),
		}),
		makeEndpointSlice("svc-def", "ns", "controller-headless", []discoveryv1.Endpoint{
			readyEndpoint("10.0.1.2", "us-east-1a"),
			readyEndpoint("10.0.2.1", "us-east-1b"),
		}),
	)

	cc := newMockCC()
	b := New(client, "ns", "controller-headless", 50051, "us-east-1a")

	r, err := b.Build(resolver.Target{}, cc, resolver.BuildOptions{})
	assert.NilError(t, err)
	defer r.Close()

	cc.waitForUpdate(t)

	got := sortedAddrs(cc.lastState())
	assert.DeepEqual(t, got, []string{"10.0.1.1:50051", "10.0.1.2:50051"})
}

func TestCloseStopsInformer(t *testing.T) {
	client := fake.NewSimpleClientset( //nolint:staticcheck // NewClientset isn't generated for discovery/v1
		makeEndpointSlice("svc-abc", "ns", "controller-headless", []discoveryv1.Endpoint{
			readyEndpoint("10.0.1.1", "us-east-1a"),
		}),
	)

	cc := newMockCC()
	b := New(client, "ns", "controller-headless", 50051, "us-east-1a")

	r, err := b.Build(resolver.Target{}, cc, resolver.BuildOptions{})
	assert.NilError(t, err)

	cc.waitForUpdate(t)

	// Close should not panic or block
	r.Close()
}

func TestNilReadyConditionTreatedAsReady(t *testing.T) {
	client := fake.NewSimpleClientset( //nolint:staticcheck // NewClientset isn't generated for discovery/v1
		makeEndpointSlice("svc-abc", "ns", "controller-headless", []discoveryv1.Endpoint{
			{
				Addresses:  []string{"10.0.1.1"},
				Conditions: discoveryv1.EndpointConditions{Ready: nil},
				Zone:       ptr.To("us-east-1a"),
			},
			readyEndpoint("10.0.1.2", "us-east-1a"),
		}),
	)

	cc := newMockCC()
	b := New(client, "ns", "controller-headless", 50051, "us-east-1a")

	r, err := b.Build(resolver.Target{}, cc, resolver.BuildOptions{})
	assert.NilError(t, err)
	defer r.Close()

	cc.waitForUpdate(t)

	got := sortedAddrs(cc.lastState())
	assert.DeepEqual(t, got, []string{"10.0.1.1:50051", "10.0.1.2:50051"})
}

func TestAllEndpointsNotReady(t *testing.T) {
	client := fake.NewSimpleClientset( //nolint:staticcheck // NewClientset isn't generated for discovery/v1
		makeEndpointSlice("svc-abc", "ns", "controller-headless", []discoveryv1.Endpoint{
			notReadyEndpoint("10.0.1.1", "us-east-1a"),
			notReadyEndpoint("10.0.1.2", "us-east-1a"),
			notReadyEndpoint("10.0.2.1", "us-east-1b"),
		}),
	)

	cc := newMockCC()
	b := New(client, "ns", "controller-headless", 50051, "us-east-1a")

	r, err := b.Build(resolver.Target{}, cc, resolver.BuildOptions{})
	assert.NilError(t, err)
	defer r.Close()

	cc.waitForUpdate(t)

	got := sortedAddrs(cc.lastState())
	assert.DeepEqual(t, got, []string{})
}

func TestEndpointSliceDeletionTriggersReResolution(t *testing.T) {
	slice1 := makeEndpointSlice("svc-abc", "ns", "controller-headless", []discoveryv1.Endpoint{
		readyEndpoint("10.0.1.1", "us-east-1a"),
	})
	slice2 := makeEndpointSlice("svc-def", "ns", "controller-headless", []discoveryv1.Endpoint{
		readyEndpoint("10.0.1.2", "us-east-1a"),
	})

	client := fake.NewSimpleClientset(slice1, slice2) //nolint:staticcheck // NewClientset isn't generated for discovery/v1

	watchReady := make(chan struct{}, 1)
	client.PrependWatchReactor("endpointslices", func(_ ktest.Action) (bool, watch.Interface, error) {
		select {
		case watchReady <- struct{}{}:
		default:
		}
		return false, nil, nil
	})

	cc := newMockCC()
	b := New(client, "ns", "controller-headless", 50051, "us-east-1a")

	r, err := b.Build(resolver.Target{}, cc, resolver.BuildOptions{})
	assert.NilError(t, err)
	defer r.Close()

	cc.waitForAddrs(t, 2)

	got := sortedAddrs(cc.lastState())
	assert.DeepEqual(t, got, []string{"10.0.1.1:50051", "10.0.1.2:50051"})

	select {
	case <-watchReady:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for watch to be established")
	}

	err = client.DiscoveryV1().EndpointSlices("ns").Delete(t.Context(),
		"svc-def", metav1.DeleteOptions{},
	)
	assert.NilError(t, err)

	cc.waitForAddrs(t, 1)

	got = sortedAddrs(cc.lastState())
	assert.DeepEqual(t, got, []string{"10.0.1.1:50051"})
}

func TestEndpointSliceModifyTriggersReResolution(t *testing.T) {
	slice := makeEndpointSlice("svc-abc", "ns", "controller-headless", []discoveryv1.Endpoint{
		readyEndpoint("10.0.1.1", "us-east-1a"),
		readyEndpoint("10.0.1.2", "us-east-1a"),
	})

	client := fake.NewSimpleClientset(slice) //nolint:staticcheck // NewClientset isn't generated for discovery/v1

	watchReady := make(chan struct{}, 1)
	client.PrependWatchReactor("endpointslices", func(_ ktest.Action) (bool, watch.Interface, error) {
		select {
		case watchReady <- struct{}{}:
		default:
		}
		return false, nil, nil
	})

	cc := newMockCC()
	b := New(client, "ns", "controller-headless", 50051, "us-east-1a")

	r, err := b.Build(resolver.Target{}, cc, resolver.BuildOptions{})
	assert.NilError(t, err)
	defer r.Close()

	cc.waitForAddrs(t, 2)

	got := sortedAddrs(cc.lastState())
	assert.DeepEqual(t, got, []string{"10.0.1.1:50051", "10.0.1.2:50051"})

	select {
	case <-watchReady:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for watch to be established")
	}

	// Update the existing slice: mark one endpoint as not-ready (simulates rolling update)
	updated := slice.DeepCopy()
	updated.Endpoints[1] = notReadyEndpoint("10.0.1.2", "us-east-1a")
	_, err = client.DiscoveryV1().EndpointSlices("ns").Update(t.Context(),
		updated, metav1.UpdateOptions{},
	)
	assert.NilError(t, err)

	cc.waitForAddrs(t, 1)

	got = sortedAddrs(cc.lastState())
	assert.DeepEqual(t, got, []string{"10.0.1.1:50051"})
}

func TestNilZoneEndpointExcludedFromSameZone(t *testing.T) {
	client := fake.NewSimpleClientset( //nolint:staticcheck // NewClientset isn't generated for discovery/v1
		makeEndpointSlice("svc-abc", "ns", "controller-headless", []discoveryv1.Endpoint{
			{
				Addresses:  []string{"10.0.0.1"},
				Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)},
				// Zone intentionally nil — not yet annotated by topology controller
			},
			readyEndpoint("10.0.1.1", "us-east-1a"),
		}),
	)

	cc := newMockCC()
	b := New(client, "ns", "controller-headless", 50051, "us-east-1a")

	r, err := b.Build(resolver.Target{}, cc, resolver.BuildOptions{})
	assert.NilError(t, err)
	defer r.Close()

	cc.waitForUpdate(t)

	// The nil-zone endpoint lands in allReady but not sameZone.
	// Since sameZone is non-empty, only the zone-matching endpoint is returned.
	got := sortedAddrs(cc.lastState())
	assert.DeepEqual(t, got, []string{"10.0.1.1:50051"})
}

func TestScheme(t *testing.T) {
	client := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset isn't generated for discovery/v1
	b := New(client, "ns", "svc", 50051, "us-east-1a")
	assert.Equal(t, b.Scheme(), "k8s")
}
