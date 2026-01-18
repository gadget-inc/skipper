package fixture

import (
	"context"
	"sync/atomic"
	"testing"

	"aidanwoods.dev/go-paseto"
	"github.com/gadget-inc/skipper/internal/function"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
)

type (
	InstanceHandler  func(ctx context.Context, fn *function.Function, excludeInstanceNames ...string) (*function.Instance, error)
	ScaleHandler     func(ctx context.Context, fn *function.Function, desiredInstances int, reason function.ScalingReason) ([]*function.Instance, error)
	HeartbeatHandler func(ctx context.Context, routerIP string, heartbeats []*function.Heartbeat, forwardedFor ...string) error
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
	t                  *testing.T
	instanceHandler    InstanceHandler
	instanceWasCalled  atomic.Bool
	scaleHandler       ScaleHandler
	scaleWasCalled     atomic.Bool
	heartbeatHandler   HeartbeatHandler
	heartbeatWasCalled atomic.Bool
}

// var _ controller.Client = &MockControllerClient{}

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

// Instance implements controller.Client.
func (f *MockControllerClient) Instance(ctx context.Context, fn *function.Function, excludeInstanceNames ...string) (instance *function.Instance, err error) {
	if f.instanceHandler == nil {
		f.t.Fatalf("mcc.Instance was called but not mocked")
	}
	f.instanceWasCalled.Store(true)
	return f.instanceHandler(ctx, fn, excludeInstanceNames...)
}

// Scale implements controller.Client.
func (f *MockControllerClient) Scale(ctx context.Context, fn *function.Function, desiredInstances int, reason function.ScalingReason) ([]*function.Instance, error) {
	if f.scaleHandler == nil {
		f.t.Fatalf("mcc.Scale was called but not mocked")
	}
	f.scaleWasCalled.Store(true)
	return f.scaleHandler(ctx, fn, desiredInstances, reason)
}

// Heartbeat implements controller.Client.
func (f *MockControllerClient) Heartbeat(ctx context.Context, routerIP string, heartbeats []*function.Heartbeat, forwardedFor ...string) error {
	if f.heartbeatHandler == nil {
		f.t.Fatalf("mcc.Heartbeat was called but not mocked")
	}
	f.heartbeatWasCalled.Store(true)
	return f.heartbeatHandler(ctx, routerIP, heartbeats, forwardedFor...)
}

func NewControllerPod() *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "controller-" + rand.String(6),
			Namespace: ControllerNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "skipper",
				"app.kubernetes.io/component": "controller",
			},
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
			PodIP: ControllerIP,
			Conditions: []v1.PodCondition{
				{
					Type:   v1.PodReady,
					Status: v1.ConditionTrue,
				},
			},
		},
	}
}
