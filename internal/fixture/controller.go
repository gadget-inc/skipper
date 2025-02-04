package fixture

import (
	"context"
	"sync"
	"testing"

	"aidanwoods.dev/go-paseto"
	"github.com/gadget-inc/fusion/internal/function"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type (
	GetHandler       func(ctx context.Context, fn function.Function) (*function.Instance, error)
	ScaleHandler     func(ctx context.Context, fn function.Function, desiredInstances int) ([]*function.Instance, error)
	HeartbeatHandler func(ctx context.Context, heartbeats []function.Heartbeat, forwardedFor ...string) error
)

const (
	DefaultControllerIP        = "127.0.0.1"
	DefaultControllerNamespace = "fusion-test"
)

var (
	DefaultControllerPasetoSecretKey = paseto.NewV2AsymmetricSecretKey()
	DefaultControllerPasetoPublicKey = DefaultControllerPasetoSecretKey.Public()
)

type MockControllerClient struct {
	t                  *testing.T
	mu                 sync.Mutex
	getHandler         GetHandler
	getWasCalled       bool
	scaleHandler       ScaleHandler
	scaleWasCalled     bool
	heartbeatHandler   HeartbeatHandler
	heartbeatWasCalled bool
}

// var _ controller.Client = &MockControllerClient{}

func NewMockControllerClient(t *testing.T) *MockControllerClient {
	mcc := &MockControllerClient{t: t}
	t.Cleanup(func() {
		if mcc.getHandler != nil && !mcc.getWasCalled {
			t.Fatalf("mcc.Get was mocked but never called")
		}
		if mcc.scaleHandler != nil && !mcc.scaleWasCalled {
			t.Fatalf("mcc.Scale was mocked but never called")
		}
		if mcc.heartbeatHandler != nil && !mcc.heartbeatWasCalled {
			t.Fatalf("mcc.Heartbeat was mocked but never called")
		}
	})
	return mcc
}

func (f *MockControllerClient) HandleGet(h GetHandler) {
	f.getHandler = h
}

func (f *MockControllerClient) HandleScale(h ScaleHandler) {
	f.scaleHandler = h
}

func (f *MockControllerClient) HandleHeartbeat(h HeartbeatHandler) {
	f.heartbeatHandler = h
}

// Get implements controller.Client.
func (f *MockControllerClient) Get(ctx context.Context, fn function.Function) (instance *function.Instance, err error) {
	if f.getHandler == nil {
		f.t.Fatalf("mcc.Get was called but not mocked")
	}
	f.getWasCalled = true
	return f.getHandler(ctx, fn)
}

// Scale implements controller.Client.
func (f *MockControllerClient) Scale(ctx context.Context, fn function.Function, desiredInstances int) ([]*function.Instance, error) {
	if f.scaleHandler == nil {
		f.t.Fatalf("mcc.Scale was called but not mocked")
	}
	f.scaleWasCalled = true
	return f.scaleHandler(ctx, fn, desiredInstances)
}

// Heartbeat implements controller.Client.
func (f *MockControllerClient) Heartbeat(ctx context.Context, heartbeats []function.Heartbeat, forwardedFor ...string) error {
	if f.heartbeatHandler == nil {
		f.t.Fatalf("mcc.Heartbeat was called but not mocked")
	}
	f.heartbeatWasCalled = true
	return f.heartbeatHandler(ctx, heartbeats, forwardedFor...)
}

func NewControllerPod() *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "controller",
			Namespace: DefaultControllerNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "fusion",
				"app.kubernetes.io/component": "controller",
			},
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
			PodIP: DefaultControllerIP,
		},
	}
}
