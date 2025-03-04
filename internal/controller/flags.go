package controller

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"aidanwoods.dev/go-paseto"
	"github.com/gadget-inc/fusion/internal/flag"
)

var (
	FlagHost = flag.Flag[string]{
		Name:        "controller-host",
		Description: "The hostname the controller listens on.",
		Default:     "0.0.0.0",
	}

	FlagPort = flag.Flag[int]{
		Name:        "controller-port",
		Description: "The port the controller listens on.",
		Default:     8080,
		Parse: func(s string) (int, error) {
			if strings.HasPrefix(s, "tcp://") && s == os.Getenv("FUSION_CONTROLLER_PORT") {
				// this environment variable was set by kubernetes, ignore it and use the default
				return 8080, nil
			}
			return strconv.Atoi(s)
		},
	}

	FlagShutdownTimeout = flag.Flag[time.Duration]{
		Name:        "controller-shutdown-timeout",
		Description: "The timeout for shutting down the controller.",
		Default:     5 * time.Second,
	}

	FlagNamespace = flag.Flag[string]{
		Name:        "controller-namespace",
		Description: "The namespace the controller is in.",
		Required:    true,
	}

	FlagPodIP = flag.Flag[string]{
		Name:        "controller-pod-ip",
		Description: "The pod IP the controller is running on.",
		Required:    true,
	}

	FlagPasetoPrivateKey = flag.Flag[paseto.V2AsymmetricSecretKey]{
		Name:        "controller-paseto-private-key",
		Description: "The private key used to sign PASETO tokens for assigned pods.",
		Required:    true,
		Sensitive:   true,
		Parse: func(s string) (paseto.V2AsymmetricSecretKey, error) {
			block, _ := pem.Decode([]byte(s))
			if block == nil {
				return paseto.NewV2AsymmetricSecretKey(), fmt.Errorf("invalid PEM block")
			}
			if block.Type != "PRIVATE KEY" {
				return paseto.NewV2AsymmetricSecretKey(), fmt.Errorf("invalid PEM block type: %s", block.Type)
			}
			key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return paseto.NewV2AsymmetricSecretKey(), fmt.Errorf("failed to parse pkcs8 private key: %w", err)
			}
			ed25519PrivateKey, ok := key.(ed25519.PrivateKey)
			if !ok {
				return paseto.NewV2AsymmetricSecretKey(), fmt.Errorf("invalid private key type")
			}
			return paseto.NewV2AsymmetricSecretKeyFromEd25519(ed25519PrivateKey)
		},
	}

	FlagHeartbeatTimeout = flag.Flag[time.Duration]{
		Name:        "controller-heartbeat-timeout",
		Description: "How long to wait before scaling a function to 0 if it has not sent a heartbeat.",
		Default:     90 * time.Second,
	}

	FlagScaleInterval = flag.Flag[time.Duration]{
		Name:        "controller-scale-interval",
		Description: "How often to scale functions.",
		Default:     15 * time.Second,
	}

	FlagHPATolerance = flag.Flag[float64]{
		Name:        "controller-hpa-tolerance",
		Description: "The usage ratio tolerance for the HPA algorithm.",
		Default:     0.1,
	}

	FlagHPAInitialReadinessDelay = flag.Flag[time.Duration]{
		Name:        "controller-hpa-initial-readiness-delay",
		Description: "The initial readiness delay for the HPA algorithm.",
		Default:     30 * time.Second,
	}

	FlagHPADownscaleStabilization = flag.Flag[time.Duration]{
		Name:        "controller-hpa-downscale-stabilization",
		Description: "The stabilization window for downscaling in the HPA algorithm.",
		Default:     90 * time.Second,
	}
)
