package fixture

import (
	"context"
	"sync/atomic"
	"testing"

	"aidanwoods.dev/go-paseto"
	"github.com/gadget-inc/skipper/internal/skipper"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
)

type (
	InstanceHandler        func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error)
	ScaleHandler           func(ctx context.Context, fn *skipper.Function, desiredInstances uint32, reason skipper.ScaleReason) ([]*skipper.Instance, error)
	HeartbeatHandler       func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error
	ReleaseInstanceHandler func(ctx context.Context, inst *skipper.Instance) error
)

const (
	ControllerIP        = "127.0.2.1"
	ControllerIP2       = "127.0.2.2"
	ControllerNamespace = "skipper-test"
)

var (
	ControllerPasetoSecretKey = paseto.NewV2AsymmetricSecretKey()
	ControllerPasetoPublicKey = ControllerPasetoSecretKey.Public()
)

type MockControllerClient struct {
	t                        *testing.T
	instanceHandler          InstanceHandler
	instanceWasCalled        atomic.Bool
	scaleHandler             ScaleHandler
	scaleWasCalled           atomic.Bool
	heartbeatHandler         HeartbeatHandler
	heartbeatWasCalled       atomic.Bool
	releaseInstanceHandler   ReleaseInstanceHandler
	releaseInstanceWasCalled atomic.Bool
}

// var _ controller.Client = &MockControllerClient{} // import cycle: verified by router_test.go usage

func NewMockControllerClient(t *testing.T) *MockControllerClient {
	mcc := &MockControllerClient{t: t}
	t.Cleanup(func() {
		if mcc.instanceHandler != nil && !mcc.instanceWasCalled.Load() {
			t.Fatalf("mcc.Instance was mocked but never called")
		}
		if mcc.scaleHandler != nil && !mcc.scaleWasCalled.Load() {
			t.Fatalf("mcc.Scale was mocked but never called")
		}
		if mcc.heartbeatHandler != nil && !mcc.heartbeatWasCalled.Load() {
			t.Fatalf("mcc.Heartbeat was mocked but never called")
		}
		if mcc.releaseInstanceHandler != nil && !mcc.releaseInstanceWasCalled.Load() {
			t.Fatalf("mcc.ReleaseInstance was mocked but never called")
		}
	})
	return mcc
}

func (f *MockControllerClient) HandleInstance(h InstanceHandler) {
	f.instanceHandler = h
}

func (f *MockControllerClient) HandleScale(h ScaleHandler) {
	f.scaleHandler = h
}

func (f *MockControllerClient) HandleHeartbeat(h HeartbeatHandler) {
	f.heartbeatHandler = h
}

// AllowHeartbeat permits heartbeat calls without requiring them.
// Use this when heartbeats may or may not occur depending on timing.
func (f *MockControllerClient) AllowHeartbeat() {
	f.heartbeatHandler = func(context.Context, string, []*skipper.Heartbeat, ...string) error {
		return nil
	}
	f.heartbeatWasCalled.Store(true) // Pre-mark as called to skip cleanup check
}

func (f *MockControllerClient) HandleReleaseInstance(h ReleaseInstanceHandler) {
	f.releaseInstanceHandler = h
}

// AllowReleaseInstance permits ReleaseInstance calls without requiring them.
// Use this when ReleaseInstance may or may not occur depending on timing.
func (f *MockControllerClient) AllowReleaseInstance() {
	f.releaseInstanceHandler = func(context.Context, *skipper.Instance) error {
		return nil
	}
	f.releaseInstanceWasCalled.Store(true) // Pre-mark as called to skip cleanup check
}

// Instance implements controller.Client.
func (f *MockControllerClient) Instance(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (instance *skipper.Instance, err error) {
	if f.instanceHandler == nil {
		f.t.Fatalf("mcc.Instance was called but not mocked")
	}
	f.instanceWasCalled.Store(true)
	return f.instanceHandler(ctx, fn, excludeInstanceNames...)
}

// Scale implements controller.Client.
func (f *MockControllerClient) Scale(ctx context.Context, fn *skipper.Function, desiredInstances uint32, reason skipper.ScaleReason) ([]*skipper.Instance, error) {
	if f.scaleHandler == nil {
		f.t.Fatalf("mcc.Scale was called but not mocked")
	}
	f.scaleWasCalled.Store(true)
	return f.scaleHandler(ctx, fn, desiredInstances, reason)
}

// Heartbeat implements controller.Client.
func (f *MockControllerClient) Heartbeat(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
	if f.heartbeatHandler == nil {
		f.t.Fatalf("mcc.Heartbeat was called but not mocked")
	}
	f.heartbeatWasCalled.Store(true)
	return f.heartbeatHandler(ctx, routerIP, heartbeats, forwardedFor...)
}

// ReleaseInstance implements controller.Client.
func (f *MockControllerClient) ReleaseInstance(ctx context.Context, inst *skipper.Instance) error {
	if f.releaseInstanceHandler == nil {
		f.t.Fatalf("mcc.ReleaseInstance was called but not mocked")
	}
	f.releaseInstanceWasCalled.Store(true)
	return f.releaseInstanceHandler(ctx, inst)
}

// Close implements controller.Client.
func (f *MockControllerClient) Close() error {
	return nil
}

// NewControllerPod builds a controller pod with the default ControllerIP and a
// random name. Use NewControllerPodAt to seed multiple peers with distinct IPs.
func NewControllerPod() *v1.Pod {
	return NewControllerPodAt("controller-"+rand.String(6), ControllerIP)
}

// NewControllerPodAt builds a controller pod with the given name and IP. Tests
// that exercise the hash ring with N peer controllers use this to seed N
// controller pods with distinct PodIPs in a single fake clientset.
func NewControllerPodAt(name, ip string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ControllerNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "skipper",
				"app.kubernetes.io/component": "controller",
			},
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
			PodIP: ip,
			Conditions: []v1.PodCondition{
				{
					Type:   v1.PodReady,
					Status: v1.ConditionTrue,
				},
			},
		},
	}
}
