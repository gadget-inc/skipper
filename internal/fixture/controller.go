package fixture

import (
	"context"
	"testing"

	"github.com/gadget-inc/fusion/internal/function"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type (
	GetHandler       func(ctx context.Context, fn function.Function) (*function.Instance, error)
	ScaleHandler     func(ctx context.Context, fn function.Function, desiredInstances int) ([]*function.Instance, error)
	HeartbeatHandler func(ctx context.Context, heartbeats []function.Heartbeat, forwardedFor ...string) error
)

type MockControllerClient struct {
	t                *testing.T
	getHandlers      map[function.Function]GetHandler
	scaleHandlers    map[function.Function]ScaleHandler
	heartbeatHandler HeartbeatHandler
}

// var _ controller.Client = &MockControllerClient{}

func NewMockControllerClient(t *testing.T) *MockControllerClient {
	return &MockControllerClient{
		t:             t,
		getHandlers:   make(map[function.Function]GetHandler),
		scaleHandlers: make(map[function.Function]ScaleHandler),
	}
}

func (f *MockControllerClient) MockGet(fn function.Function, h GetHandler) {
	f.getHandlers[fn] = h
}

func (f *MockControllerClient) HandleScale(fn function.Function, h ScaleHandler) {
	f.scaleHandlers[fn] = h
}

func (f *MockControllerClient) HandleHeartbeat(h HeartbeatHandler) {
	f.heartbeatHandler = h
}

// Get implements controller.Client.
func (f *MockControllerClient) Get(ctx context.Context, fn function.Function) (instance *function.Instance, err error) {
	if h, ok := f.getHandlers[fn]; ok {
		return h(ctx, fn)
	}
	f.t.Fatalf("no get handler for function: %v", fn)
	return nil, nil
}

// Heartbeat implements controller.Client.
func (f *MockControllerClient) Heartbeat(ctx context.Context, heartbeats []function.Heartbeat, forwardedFor ...string) error {
	return f.heartbeatHandler(ctx, heartbeats, forwardedFor...)
}

// Scale implements controller.Client.
func (f *MockControllerClient) Scale(ctx context.Context, fn function.Function, desiredInstances int) ([]*function.Instance, error) {
	if h, ok := f.scaleHandlers[fn]; ok {
		return h(ctx, fn, desiredInstances)
	}
	f.t.Fatalf("no scale handler for function: %v", fn)
	return nil, nil
}

const (
	DefaultControllerIP        = "127.0.0.1"
	DefaultControllerNamespace = "fusion-test"
)

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
