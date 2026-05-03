package cmd

import (
	"errors"
	"path/filepath"

	"aidanwoods.dev/go-paseto"
	"github.com/gadget-inc/skipper/internal/cmd/fixture"
	"github.com/gadget-inc/skipper/internal/controller"
	"github.com/gadget-inc/skipper/internal/router"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	kubernetesmetrics "k8s.io/metrics/pkg/client/clientset/versioned"
)

// ControllerDeps holds injectable dependencies for the controller command.
// This allows testing without real Kubernetes clusters or network connections.
type ControllerDeps struct {
	// LoadKubeConfig loads the Kubernetes configuration.
	// Defaults to trying in-cluster config, falling back to ~/.kube/config.
	LoadKubeConfig func() (*rest.Config, error)

	// NewK8sClient creates a Kubernetes client from the given config.
	NewK8sClient func(*rest.Config) (kubernetes.Interface, error)

	// NewMetricsClient creates a Kubernetes metrics client from the given config.
	NewMetricsClient func(*rest.Config) (kubernetesmetrics.Interface, error)

	// NewClientFunc returns a function that creates controller clients for inter-controller communication.
	NewClientFunc func(port int) controller.NewClientFunc
}

// DefaultControllerDeps returns the default production dependencies.
func DefaultControllerDeps() *ControllerDeps {
	return &ControllerDeps{
		LoadKubeConfig:   defaultLoadKubeConfig,
		NewK8sClient:     defaultNewK8sClient,
		NewMetricsClient: defaultNewMetricsClient,
		NewClientFunc:    defaultNewClientFunc,
	}
}

func defaultLoadKubeConfig() (*rest.Config, error) {
	kubeConfig, err := rest.InClusterConfig()
	if errors.Is(err, rest.ErrNotInCluster) {
		kubeConfig, err = clientcmd.BuildConfigFromFlags("", filepath.Join(homedir.HomeDir(), ".kube", "config"))
	}
	return kubeConfig, err
}

func defaultNewK8sClient(config *rest.Config) (kubernetes.Interface, error) {
	return kubernetes.NewForConfig(config)
}

func defaultNewMetricsClient(config *rest.Config) (kubernetesmetrics.Interface, error) {
	return kubernetesmetrics.NewForConfig(config)
}

func defaultNewClientFunc(port int) controller.NewClientFunc {
	return func(host string) controller.Client {
		client, err := controller.NewClient(host, port)
		if err != nil {
			// NewClient only fails for configuration errors (grpc.NewClient is lazy)
			panic("failed to create client: " + err.Error())
		}
		return client
	}
}

// RouterDeps holds injectable dependencies for the router command.
type RouterDeps struct {
	// NewControllerClient creates a controller client based on the router config.
	// Returns the client and any error encountered.
	NewControllerClient func(cfg *router.Config) (controller.Client, error)
}

// DefaultRouterDeps returns the default production dependencies.
func DefaultRouterDeps() *RouterDeps {
	return &RouterDeps{
		NewControllerClient: defaultNewControllerClient,
	}
}

func defaultNewControllerClient(cfg *router.Config) (controller.Client, error) {
	// Use the headless service host if configured,
	// otherwise fall back to the standard service host
	host := cfg.ControllerHeadlessServiceHost
	if host == "" {
		host = cfg.ControllerServiceHost
	}
	return controller.NewClient(host, cfg.ControllerPort)
}

// FixtureDeps holds injectable dependencies for the fixture command.
// Public-key parsing and token-path resolution route through these so tests
// can substitute pre-built keys and per-test temp paths.
type FixtureDeps struct {
	// LoadPublicKey parses a PASETO V2 public key from PEM bytes.
	// Defaults to fixture.ParsePublicKeyPEM (SPKI ed25519).
	LoadPublicKey func(pemBytes []byte) (paseto.V2AsymmetricPublicKey, error)

	// ResolveTokenPath returns the on-disk path where the fixture persists
	// its assigned token. Defaults to the configured value.
	ResolveTokenPath func(configured string) string
}

// DefaultFixtureDeps returns the default production dependencies.
func DefaultFixtureDeps() *FixtureDeps {
	return &FixtureDeps{
		LoadPublicKey:    fixture.ParsePublicKeyPEM,
		ResolveTokenPath: func(configured string) string { return configured },
	}
}
