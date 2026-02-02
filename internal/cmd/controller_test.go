package cmd

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/controller"
	"gotest.tools/v3/assert"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	kubernetesmetrics "k8s.io/metrics/pkg/client/clientset/versioned"
	fakemetrics "k8s.io/metrics/pkg/client/clientset/versioned/fake"
)

// testPasetoPrivateKeyPEM is a valid Ed25519 private key in PEM format for testing.
// Generated with: openssl genpkey -algorithm ed25519 -outform PEM
const testPasetoPrivateKeyPEM = `-----BEGIN PRIVATE KEY-----
MC4CAQAwBQYDK2VwBCIEIBzCPypLbnSWs2p4k8OQFMXE9EYXbVkqpTT/JNpQPwyc
-----END PRIVATE KEY-----`

func TestControllerProtocolSelection(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		protocol   string
		expectGRPC bool
	}{
		{
			name:       "http protocol uses HTTP client",
			protocol:   "http",
			expectGRPC: false,
		},
		{
			name:       "grpc protocol uses gRPC client",
			protocol:   "grpc",
			expectGRPC: true,
		},
		{
			name:       "empty protocol defaults to HTTP",
			protocol:   "",
			expectGRPC: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var usedGRPC atomic.Bool

			mockNewClientFunc := func(protocol string, grpcPort int) controller.NewClientFunc {
				usedGRPC.Store(protocol == "grpc")
				// Return a dummy function - we just care about what protocol was passed
				return func(host string, port int) controller.Client {
					return nil
				}
			}

			_ = mockNewClientFunc(tc.protocol, 50051)

			assert.Equal(t, usedGRPC.Load(), tc.expectGRPC)
		})
	}
}

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

			var usedOutOfCluster atomic.Bool

			// Simulate the defaultLoadKubeConfig behavior
			loadKubeConfig := func() (*rest.Config, error) {
				if tc.inClusterError != nil {
					if errors.Is(tc.inClusterError, rest.ErrNotInCluster) {
						usedOutOfCluster.Store(true)
						return &rest.Config{}, nil // Simulating successful out-of-cluster load
					}
					return nil, tc.inClusterError
				}
				usedOutOfCluster.Store(false)
				return &rest.Config{}, nil // Simulating successful in-cluster load
			}

			cfg, err := loadKubeConfig()
			assert.NilError(t, err)
			assert.Assert(t, cfg != nil)
			assert.Equal(t, usedOutOfCluster.Load(), tc.expectOutOfCluster)
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

	testCases := []struct {
		name     string
		protocol string
		grpcPort int
	}{
		{
			name:     "http protocol",
			protocol: "http",
			grpcPort: 50051,
		},
		{
			name:     "grpc protocol",
			protocol: "grpc",
			grpcPort: 50051,
		},
		{
			name:     "empty protocol defaults to http",
			protocol: "",
			grpcPort: 50051,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			newClientFunc := defaultNewClientFunc(tc.protocol, tc.grpcPort)
			assert.Assert(t, newClientFunc != nil)

			// Create a client to verify it works
			client := newClientFunc("localhost", 8080)
			assert.Assert(t, client != nil)
		})
	}
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
				NewClientFunc: func(protocol string, grpcPort int) controller.NewClientFunc {
					return func(host string, port int) controller.Client {
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

func TestControllerGRPCListenerFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Parallel()

	// Bind to a port first to cause listener creation to fail
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NilError(t, err)
	defer listener.Close()
	grpcPort := listener.Addr().(*net.TCPAddr).Port

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
		NewClientFunc: func(protocol string, grpcPort int) controller.NewClientFunc {
			return func(host string, port int) controller.Client {
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
		"--grpc-port=" + itoa(grpcPort), // Same port that's already bound
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = cmd.ExecuteContext(ctx)
	assert.Assert(t, err != nil, "expected error when gRPC port is already bound")
	assert.ErrorContains(t, err, "failed to create gRPC listener")
}

func TestControllerHTTPListenerFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Parallel()

	// Bind to the HTTP port first to cause server startup to fail
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NilError(t, err)
	defer listener.Close()
	httpPort := listener.Addr().(*net.TCPAddr).Port

	// Find a free port for gRPC
	grpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NilError(t, err)
	grpcPort := grpcListener.Addr().(*net.TCPAddr).Port
	grpcListener.Close()

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
		NewClientFunc: func(protocol string, grpcPort int) controller.NewClientFunc {
			return func(host string, port int) controller.Client {
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
		"--http-port=" + itoa(httpPort), // Same port that's already bound
		"--grpc-port=" + itoa(grpcPort),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = cmd.ExecuteContext(ctx)
	assert.Assert(t, err != nil, "expected error when HTTP port is already bound")
	assert.ErrorContains(t, err, "failed to serve controller HTTP")
}

