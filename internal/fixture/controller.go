package fixture

import (
	"context"
	"sync"
	"testing"

	"github.com/gadget-inc/fusion/internal/function"
)

type (
	GetHandler   func(ctx context.Context, fn function.Function) (*function.Instance, error)
	ScaleHandler func(ctx context.Context, fn function.Function, desiredInstances int) ([]*function.Instance, error)
)

type MockControllerClient struct {
	t             *testing.T
	mu            sync.Mutex
	heartbeats    []function.Heartbeat
	getHandlers   map[function.Function]GetHandler
	scaleHandlers map[function.Function]ScaleHandler
}

// var _ controller.Client = &MockControllerClient{}

func NewMockControllerClient(t *testing.T) *MockControllerClient {
	return &MockControllerClient{
		t:             t,
		getHandlers:   make(map[function.Function]GetHandler),
		scaleHandlers: make(map[function.Function]ScaleHandler),
	}
}

func (f *MockControllerClient) HandleGet(fn function.Function, h GetHandler) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getHandlers[fn] = h
}

func (f *MockControllerClient) HandleScale(fn function.Function, h ScaleHandler) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scaleHandlers[fn] = h
}

func (f *MockControllerClient) Heartbeats() []function.Heartbeat {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.heartbeats
}

// Get implements controller.Client.
func (f *MockControllerClient) Get(ctx context.Context, fn function.Function) (instance *function.Instance, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if h, ok := f.getHandlers[fn]; ok {
		return h(ctx, fn)
	}

	f.t.Fatalf("no get handler for function: %v", fn)

	return nil, nil
}

// Heartbeat implements controller.Client.
func (f *MockControllerClient) Heartbeat(ctx context.Context, heartbeats []function.Heartbeat) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.heartbeats = append(f.heartbeats, heartbeats...)
	return nil
}

// Scale implements controller.Client.
func (f *MockControllerClient) Scale(ctx context.Context, fn function.Function, desiredInstances int) ([]*function.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if h, ok := f.scaleHandlers[fn]; ok {
		return h(ctx, fn, desiredInstances)
	}

	f.t.Fatalf("no scale handler for function: %v", fn)

	return nil, nil
}
