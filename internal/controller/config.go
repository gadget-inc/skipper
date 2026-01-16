package controller

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"aidanwoods.dev/go-paseto"
)

// Config holds the controller configuration.
type Config struct {
	Host                           string           `flag:"host" description:"The hostname the controller listens on." default:"0.0.0.0"`
	Port                           int              `flag:"port" description:"The port the controller listens on." default:"8080"`
	ShutdownTimeout                time.Duration    `flag:"shutdown-timeout" description:"The timeout for shutting down the controller." default:"5s"`
	Namespace                      string           `flag:"namespace" description:"The namespace the controller is in." required:"true"`
	PodIP                          string           `flag:"pod-ip" description:"The pod IP the controller is running on." required:"true"`
	KubeConfigQPS                  float32          `flag:"kubeconfig-qps" description:"The QPS for the kubeconfig client." default:"100"`
	KubeConfigBurst                int              `flag:"kubeconfig-burst" description:"The burst for the kubeconfig client." default:"200"`
	PasetoPrivateKey               PasetoPrivateKey `flag:"paseto-private-key" description:"The private key used to sign PASETO tokens for assigned pods." required:"true" sensitive:"true"`
	HeartbeatTimeout               time.Duration    `flag:"heartbeat-timeout" description:"How long to wait before scaling a function to 0 if it has not sent a heartbeat." default:"90s"`
	ScaleInterval                  time.Duration    `flag:"scale-interval" description:"How often to scale functions." default:"15s"`
	HPATolerance                   float64          `flag:"hpa-tolerance" description:"The usage ratio tolerance for the HPA algorithm." default:"0.1"`
	HPAInitialReadinessDelay       time.Duration    `flag:"hpa-initial-readiness-delay" description:"The initial readiness delay for the HPA algorithm." default:"30s"`
	HPADownscaleStabilization      time.Duration    `flag:"hpa-downscale-stabilization" description:"The stabilization window for downscaling in the HPA algorithm." default:"90s"`
	AvailableReplicaDivisor        float32          `flag:"available-replica-divisor" description:"The divisor for the number of available replicas needed to terminate a stale instance." default:"1.2"`
	HashRingWaitTime               time.Duration    `flag:"hash-ring-wait-time" description:"How long to wait for the controller to populate its hash ring." default:"10s"`
	FunctionNamespaces             []string         `flag:"function-namespaces" description:"The namespaces where functions can be invoked." required:"true"`
	FunctionAssignPath             string           `flag:"function-assign-path" description:"The path used to assign a function to a pod." default:"/__skipper/assign"`
	FunctionAssignTimeout          time.Duration    `flag:"function-assign-timeout" description:"The timeout for assigning a function to a pod." default:"30s"`
	MaxConcurrentStaleReplacements int              `flag:"max-concurrent-stale-replacements" description:"Maximum number of stale instances that can be replaced concurrently." default:"10"`
	SkipForbiddenNamespaces        bool             `flag:"skip-forbidden-namespaces" description:"Whether to skip function namespaces that the service account does not have access to." default:"false"`
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.AvailableReplicaDivisor <= 1 {
		return errors.New("available replica divisor must be greater than 1")
	}
	if c.MaxConcurrentStaleReplacements < 1 {
		return errors.New("max concurrent stale replacements must be at least 1")
	}
	return nil
}

// PasetoPrivateKey wraps paseto.V2AsymmetricSecretKey with text unmarshaling support.
type PasetoPrivateKey struct {
	paseto.V2AsymmetricSecretKey
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (k *PasetoPrivateKey) UnmarshalText(text []byte) error {
	block, _ := pem.Decode(text)
	if block == nil {
		return errors.New("invalid PEM block")
	}
	if block.Type != "PRIVATE KEY" {
		return fmt.Errorf("invalid PEM block type: %s", block.Type)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse pkcs8 private key: %w", err)
	}
	ed25519PrivateKey, ok := key.(ed25519.PrivateKey)
	if !ok {
		return errors.New("invalid private key type")
	}
	k.V2AsymmetricSecretKey, err = paseto.NewV2AsymmetricSecretKeyFromEd25519(ed25519PrivateKey)
	return err
}
