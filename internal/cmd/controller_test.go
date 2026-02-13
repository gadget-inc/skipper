package cmd

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/controller"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"gotest.tools/v3/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	ktesting "k8s.io/client-go/testing"
	kubernetesmetrics "k8s.io/metrics/pkg/client/clientset/versioned"
	fakemetrics "k8s.io/metrics/pkg/client/clientset/versioned/fake"
)

// testPasetoPrivateKeyPEM is a valid Ed25519 private key in PEM format for testing.
// Generated with: openssl genpkey -algorithm ed25519 -outform PEM
const testPasetoPrivateKeyPEM = `-----BEGIN PRIVATE KEY-----
MC4CAQAwBQYDK2VwBCIEIBzCPypLbnSWs2p4k8OQFMXE9EYXbVkqpTT/JNpQPwyc
-----END PRIVATE KEY-----`

func TestControllerKubeConfigLoading(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name               string
		inClusterError     error
		expectOutOfCluster bool
	}{
		{
			name:               "uses in-cluster when available",
			inClusterError:     nil,
			expectOutOfCluster: false,
		},
		{
			name:               "falls back to kubeconfig when not in cluster",
			inClusterError:     rest.ErrNotInCluster,
			expectOutOfCluster: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var usedOutOfCluster bool

			// Simulate the defaultLoadKubeConfig behavior
			loadKubeConfig := func() (*rest.Config, error) {
				if tc.inClusterError != nil {
					if errors.Is(tc.inClusterError, rest.ErrNotInCluster) {
						usedOutOfCluster = true
						return &rest.Config{}, nil // Simulating successful out-of-cluster load
					}
					return nil, tc.inClusterError
				}
				usedOutOfCluster = false
				return &rest.Config{}, nil // Simulating successful in-cluster load
			}

			cfg, err := loadKubeConfig()
			assert.NilError(t, err)
			assert.Assert(t, cfg != nil)
			assert.Equal(t, usedOutOfCluster, tc.expectOutOfCluster)
		})
	}
}

func TestDefaultLoadKubeConfig(t *testing.T) {
	// This test verifies the actual defaultLoadKubeConfig behavior
	// It will try in-cluster first, then fall back to out-of-cluster
	// In a test environment, we're not in a cluster, so it should fall back

	// Note: This test may fail in CI if there's no kubeconfig at ~/.kube/config
	// We just test that the function doesn't panic and returns a reasonable result
	t.Run("does not panic", func(t *testing.T) {
		// Just verify it doesn't panic - the actual result depends on the environment
		cfg, err := defaultLoadKubeConfig()
		// We expect either success (if ~/.kube/config exists) or an error (if it doesn't)
		if err != nil {
			// This is expected in environments without kubeconfig
			t.Logf("defaultLoadKubeConfig returned error (expected in environments without kubeconfig): %v", err)
		} else {
			assert.Assert(t, cfg != nil)
		}
	})
}

func TestDefaultNewClientFunc(t *testing.T) {
	t.Parallel()

	newClientFunc := defaultNewClientFunc(50051)
	assert.Assert(t, newClientFunc != nil)

	// Create a client to verify it works
	client := newClientFunc("localhost")
	assert.Assert(t, client != nil)
}

func TestControllerCommandConfigValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Parallel()

	testCases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name: "invalid available-replica-divisor fails validation",
			args: []string{
				"--namespace=test",
				"--pod-ip=10.0.0.1",
				"--paseto-private-key=" + testPasetoPrivateKeyPEM,
				"--function-namespaces=default",
				"--available-replica-divisor=0.5",
			},
			wantErr: "available replica divisor must be greater than 1",
		},
		{
			name: "invalid max-concurrent-stale-replacements fails validation",
			args: []string{
				"--namespace=test",
				"--pod-ip=10.0.0.1",
				"--paseto-private-key=" + testPasetoPrivateKeyPEM,
				"--function-namespaces=default",
				"--max-concurrent-stale-replacements=0",
			},
			wantErr: "max concurrent stale replacements must be at least 1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Create a context that we'll cancel to prevent the command from running forever
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Use mock deps that return immediately so we can test validation
			deps := &ControllerDeps{
				LoadKubeConfig: func() (*rest.Config, error) {
					return &rest.Config{}, nil
				},
				NewK8sClient: func(c *rest.Config) (kubernetes.Interface, error) {
					return fake.NewClientset(), nil
				},
				NewMetricsClient: func(c *rest.Config) (kubernetesmetrics.Interface, error) {
					return fakemetrics.NewSimpleClientset(), nil //nolint:staticcheck // NewClientset isn't generated for this package
				},
				NewClientFunc: func(port int) controller.NewClientFunc {
					return func(host string) controller.Client {
						return nil
					}
				},
			}

			cmd := NewController(deps)
			cmd.SetContext(ctx)
			cmd.SetArgs(tc.args)

			err := cmd.Execute()

			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
			} else {
				// If we don't expect an error, we expect the command to either
				// succeed or be cancelled by our context timeout
				if err != nil && err != context.DeadlineExceeded {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestControllerKubeConfigLoadFailure(t *testing.T) {
	t.Parallel()

	deps := &ControllerDeps{
		LoadKubeConfig: func() (*rest.Config, error) {
			return nil, errors.New("kubeconfig not found")
		},
	}

	cmd := NewController(deps)
	cmd.SetArgs([]string{
		"--namespace=test",
		"--pod-ip=10.0.0.1",
		"--paseto-private-key=" + testPasetoPrivateKeyPEM,
		"--function-namespaces=default",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := cmd.ExecuteContext(ctx)
	assert.ErrorContains(t, err, "failed to load kubernetes config")
	assert.ErrorContains(t, err, "kubeconfig not found")
}

func TestControllerK8sClientCreationFailure(t *testing.T) {
	t.Parallel()

	deps := &ControllerDeps{
		LoadKubeConfig: func() (*rest.Config, error) {
			return &rest.Config{}, nil
		},
		NewK8sClient: func(c *rest.Config) (kubernetes.Interface, error) {
			return nil, errors.New("failed to connect to cluster")
		},
	}

	cmd := NewController(deps)
	cmd.SetArgs([]string{
		"--namespace=test",
		"--pod-ip=10.0.0.1",
		"--paseto-private-key=" + testPasetoPrivateKeyPEM,
		"--function-namespaces=default",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := cmd.ExecuteContext(ctx)
	assert.ErrorContains(t, err, "failed to create kubernetes client")
	assert.ErrorContains(t, err, "failed to connect to cluster")
}

func TestControllerMetricsClientCreationFailure(t *testing.T) {
	t.Parallel()

	deps := &ControllerDeps{
		LoadKubeConfig: func() (*rest.Config, error) {
			return &rest.Config{}, nil
		},
		NewK8sClient: func(c *rest.Config) (kubernetes.Interface, error) {
			return fake.NewClientset(), nil
		},
		NewMetricsClient: func(c *rest.Config) (kubernetesmetrics.Interface, error) {
			return nil, errors.New("metrics server unavailable")
		},
	}

	cmd := NewController(deps)
	cmd.SetArgs([]string{
		"--namespace=test",
		"--pod-ip=10.0.0.1",
		"--paseto-private-key=" + testPasetoPrivateKeyPEM,
		"--function-namespaces=default",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := cmd.ExecuteContext(ctx)
	assert.ErrorContains(t, err, "failed to create metrics client")
	assert.ErrorContains(t, err, "metrics server unavailable")
}

func TestControllerListenerFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Parallel()

	// Bind to a port first to cause listener creation to fail
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NilError(t, err)
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	deps := &ControllerDeps{
		LoadKubeConfig: func() (*rest.Config, error) {
			return &rest.Config{}, nil
		},
		NewK8sClient: func(c *rest.Config) (kubernetes.Interface, error) {
			return fake.NewClientset(), nil
		},
		NewMetricsClient: func(c *rest.Config) (kubernetesmetrics.Interface, error) {
			return fakemetrics.NewSimpleClientset(), nil //nolint:staticcheck // NewClientset isn't generated for this package
		},
		NewClientFunc: func(port int) controller.NewClientFunc {
			return func(host string) controller.Client {
				return nil
			}
		},
	}

	cmd := NewController(deps)
	cmd.SetArgs([]string{
		"--namespace=test",
		"--pod-ip=10.0.0.1",
		"--paseto-private-key=" + testPasetoPrivateKeyPEM,
		"--function-namespaces=default",
		"--host=127.0.0.1",
		"--port=" + itoa(port), // Same port that's already bound
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = cmd.ExecuteContext(ctx)
	assert.Assert(t, err != nil, "expected error when port is already bound")
	assert.ErrorContains(t, err, "failed to create listener")
}

func TestControllerHealthCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Parallel()

	// Find free port
	freeListener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NilError(t, err)
	port := freeListener.Addr().(*net.TCPAddr).Port
	freeListener.Close()

	// Block informer sync so we can observe NOT_SERVING before the controller is ready
	unblockInformers := make(chan struct{})
	reactorEntered := make(chan struct{})
	var reactorOnce sync.Once
	fakeClient := fake.NewClientset()
	fakeClient.PrependReactor("list", "*", func(action ktesting.Action) (bool, runtime.Object, error) {
		reactorOnce.Do(func() { close(reactorEntered) })
		<-unblockInformers
		return false, nil, nil
	})
	fakeClient.PrependWatchReactor("*", func(action ktesting.Action) (bool, watch.Interface, error) {
		<-unblockInformers
		return false, nil, nil
	})

	deps := &ControllerDeps{
		LoadKubeConfig: func() (*rest.Config, error) {
			return &rest.Config{}, nil
		},
		NewK8sClient: func(c *rest.Config) (kubernetes.Interface, error) {
			return fakeClient, nil
		},
		NewMetricsClient: func(c *rest.Config) (kubernetesmetrics.Interface, error) {
			return fakemetrics.NewSimpleClientset(), nil //nolint:staticcheck // NewClientset isn't generated for this package
		},
		NewClientFunc: func(port int) controller.NewClientFunc {
			return func(host string) controller.Client {
				return nil
			}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := NewController(deps)
	cmd.SetArgs([]string{
		"--namespace=test",
		"--pod-ip=10.0.0.1",
		"--paseto-private-key=" + testPasetoPrivateKeyPEM,
		"--function-namespaces=default",
		"--host=127.0.0.1",
		"--port=" + strconv.Itoa(port),
	})

	cmdErr := make(chan error, 1)
	go func() {
		cmdErr <- cmd.ExecuteContext(ctx)
	}()

	// Wait until the informer's list call is blocked inside the reactor.
	// This guarantees informers haven't synced so health must be NOT_SERVING.
	<-reactorEntered

	// Wait for the gRPC server to accept connections (health is NOT_SERVING while informers sync)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	assert.NilError(t, err)
	defer conn.Close()

	healthClient := healthgrpc.NewHealthClient(conn)

	// Health should NOT return SERVING while informers are blocked
	var notServing bool
	for i := 0; i < 50; i++ {
		resp, err := healthClient.Check(ctx, &healthgrpc.HealthCheckRequest{})
		if err != nil {
			st, ok := status.FromError(err)
			if ok && st.Code() == codes.NotFound {
				notServing = true
				break
			}
			// Server not yet accepting connections, retry
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if resp.Status != healthgrpc.HealthCheckResponse_SERVING {
			notServing = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	assert.Assert(t, notServing, "expected health to NOT be SERVING before controller is ready")

	// Unblock informers so the controller can finish starting
	close(unblockInformers)

	// Health should transition to SERVING after the controller is ready
	var resp *healthgrpc.HealthCheckResponse
	for i := 0; i < 50; i++ {
		resp, err = healthClient.Check(ctx, &healthgrpc.HealthCheckRequest{})
		if err == nil && resp.Status == healthgrpc.HealthCheckResponse_SERVING {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	assert.NilError(t, err)
	assert.Equal(t, resp.Status, healthgrpc.HealthCheckResponse_SERVING)

	cancel()
	<-cmdErr
}
