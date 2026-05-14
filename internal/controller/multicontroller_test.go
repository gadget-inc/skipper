package controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/gadget-inc/skipper/internal/fixture"
	"gotest.tools/v3/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

// multiControllerFixture holds N Controller instances backed by a single fake
// clientset, with their hash rings populated to agree on all N peer IPs.
type multiControllerFixture struct {
	fakeKubernetes *fake.Clientset
	controllers    []*Controller
	cfgs           []*Config
	peerIPs        []string
}

// newMultiControllerFixture seeds n controller pods with distinct peer IPs
// into a single fake clientset, builds n Controllers each pointed at a peer
// IP, and starts informers so every controller's ring contains all n peers.
//
// Use 127.0.2.{1..n} for peer IPs so they don't collide with loopback servers
// (httptest binds 127.0.0.1) or the ControllerIP/ControllerIP2 constants used
// elsewhere in fixture.
//
// extraObjects are seeded into the fake clientset's tracker before informers
// start, so initial-list picks them up without filling the fake watch channel.
// Callers that need a large pre-populated pool (e.g. benchmarks) MUST pass
// pods through this slice rather than calling Tracker().Add after the fixture
// is built.
func newMultiControllerFixture(tb testing.TB, ctx context.Context, n int, extraObjects ...runtime.Object) *multiControllerFixture {
	tb.Helper()

	peerIPs := make([]string, n)
	seedObjs := make([]runtime.Object, 0, n+len(extraObjects))
	for i := range peerIPs {
		peerIPs[i] = fmt.Sprintf("127.0.2.%d", i+1)
		seedObjs = append(seedObjs, fixture.NewControllerPodAt(fmt.Sprintf("controller-%d", i+1), peerIPs[i]))
	}
	seedObjs = append(seedObjs, extraObjects...)

	fakeKubernetes := fake.NewClientset(seedObjs...)

	cfgs := make([]*Config, n)
	controllers := make([]*Controller, n)
	for i, ip := range peerIPs {
		cfg := testConfig()
		cfg.PodIP = ip
		cfgs[i] = cfg
		controllers[i] = New(cfg, nil, fakeKubernetes, nil)

		err := controllers[i].startInformers(ctx)
		assert.NilError(tb, err, "controller %d failed to start informers", i)

		assert.Equal(tb, len(controllers[i].ring.List()), n,
			"controller %d ring should have %d peers, got %v", i, n, controllers[i].ring.List())
	}

	return &multiControllerFixture{
		fakeKubernetes: fakeKubernetes,
		controllers:    controllers,
		cfgs:           cfgs,
		peerIPs:        peerIPs,
	}
}
